package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// DiskCache stores cached responses as files on disk.
type DiskCache struct {
	basePath      string
	maxSize       int64
	maxObjectSize int64
	currentSize   atomic.Int64
	mu            sync.RWMutex
	index         map[string]*diskMeta // hash -> metadata
	stopCh        chan struct{}
}

type diskMeta struct {
	Hash       string    `json:"hash"`
	Key        string    `json:"key"`
	Size       int64     `json:"size"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastAccess time.Time `json:"last_access"`
}

// NewDiskCache creates a new disk-based cache.
func NewDiskCache(basePath string, maxSize, maxObjectSize int64) (*DiskCache, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	dc := &DiskCache{
		basePath:      basePath,
		maxSize:       maxSize,
		maxObjectSize: maxObjectSize,
		index:         make(map[string]*diskMeta),
		stopCh:        make(chan struct{}),
	}

	// Build index from existing files in background
	go dc.buildIndex()

	// Start eviction goroutine
	go dc.evictionLoop()

	return dc, nil
}

func (dc *DiskCache) Get(key string) (*Entry, bool) {
	hash := HashKey(key)

	dc.mu.RLock()
	meta, exists := dc.index[hash]
	dc.mu.RUnlock()

	if !exists {
		return nil, false
	}

	if time.Now().After(meta.ExpiresAt) {
		dc.Delete(key)
		return nil, false
	}

	dataPath := dc.dataPath(hash)
	bodyData, err := os.ReadFile(dataPath)
	if err != nil {
		dc.Delete(key)
		return nil, false
	}

	metaPath := dc.metaPath(hash)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		dc.Delete(key)
		return nil, false
	}

	var entry Entry
	if err := json.Unmarshal(metaData, &entry); err != nil {
		dc.Delete(key)
		return nil, false
	}
	entry.Body = bodyData

	// Update last access time async
	go func() {
		dc.mu.Lock()
		if m, ok := dc.index[hash]; ok {
			m.LastAccess = time.Now()
		}
		dc.mu.Unlock()
	}()

	return &entry, true
}

func (dc *DiskCache) Put(key string, entry *Entry) error {
	if entry.Size > dc.maxObjectSize {
		return nil // too large, skip
	}

	hash := HashKey(key)
	dir := dc.dirPath(hash)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	// Write body to temp file, then rename (atomic)
	dataPath := dc.dataPath(hash)
	tmpPath := dataPath + ".tmp"
	if err := os.WriteFile(tmpPath, entry.Body, 0644); err != nil {
		return fmt.Errorf("write cache body: %w", err)
	}
	if err := os.Rename(tmpPath, dataPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename cache body: %w", err)
	}

	// Write metadata
	entryCopy := *entry
	entryCopy.Body = nil // don't store body in metadata
	metaData, err := json.Marshal(&entryCopy)
	if err != nil {
		return fmt.Errorf("marshal cache meta: %w", err)
	}
	metaPath := dc.metaPath(hash)
	tmpMetaPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpMetaPath, metaData, 0644); err != nil {
		return fmt.Errorf("write cache meta: %w", err)
	}
	if err := os.Rename(tmpMetaPath, metaPath); err != nil {
		os.Remove(tmpMetaPath)
		return fmt.Errorf("rename cache meta: %w", err)
	}

	// Update index
	dc.mu.Lock()
	oldMeta, existed := dc.index[hash]
	if existed {
		dc.currentSize.Add(-oldMeta.Size)
	}
	dc.index[hash] = &diskMeta{
		Hash:       hash,
		Key:        key,
		Size:       entry.Size,
		ExpiresAt:  entry.ExpiresAt,
		LastAccess: time.Now(),
	}
	dc.currentSize.Add(entry.Size)
	dc.mu.Unlock()

	return nil
}

func (dc *DiskCache) Delete(key string) {
	hash := HashKey(key)

	dc.mu.Lock()
	meta, exists := dc.index[hash]
	if exists {
		delete(dc.index, hash)
		dc.currentSize.Add(-meta.Size)
	}
	dc.mu.Unlock()

	if exists {
		os.Remove(dc.dataPath(hash))
		os.Remove(dc.metaPath(hash))
	}
}

func (dc *DiskCache) Purge(prefix string) int {
	dc.mu.Lock()
	var toDelete []string
	for hash, meta := range dc.index {
		if prefix == "" || meta.Key == prefix ||
			(len(meta.Key) > len(prefix) && meta.Key[:len(prefix)] == prefix) {
			toDelete = append(toDelete, hash)
		}
	}
	for _, hash := range toDelete {
		meta := dc.index[hash]
		dc.currentSize.Add(-meta.Size)
		delete(dc.index, hash)
	}
	dc.mu.Unlock()

	for _, hash := range toDelete {
		os.Remove(dc.dataPath(hash))
		os.Remove(dc.metaPath(hash))
	}
	return len(toDelete)
}

func (dc *DiskCache) Stop() {
	close(dc.stopCh)
}

func (dc *DiskCache) dirPath(hash string) string {
	return filepath.Join(dc.basePath, hash[0:2], hash[2:4])
}

func (dc *DiskCache) dataPath(hash string) string {
	return filepath.Join(dc.dirPath(hash), hash+".data")
}

func (dc *DiskCache) metaPath(hash string) string {
	return filepath.Join(dc.dirPath(hash), hash+".meta")
}

func (dc *DiskCache) buildIndex() {
	filepath.Walk(dc.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".meta" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil
		}
		hash := HashKey(entry.Key)
		dc.mu.Lock()
		dc.index[hash] = &diskMeta{
			Hash:       hash,
			Key:        entry.Key,
			Size:       entry.Size,
			ExpiresAt:  entry.ExpiresAt,
			LastAccess: time.Now(),
		}
		dc.currentSize.Add(entry.Size)
		dc.mu.Unlock()
		return nil
	})
	dc.mu.RLock()
	count := len(dc.index)
	dc.mu.RUnlock()
	cdnlog.Info("disk cache index built", "entries", count, "size", dc.currentSize.Load())
}

func (dc *DiskCache) evictionLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dc.stopCh:
			return
		case <-ticker.C:
			dc.evict()
		}
	}
}

func (dc *DiskCache) evict() {
	// Evict expired entries
	dc.mu.Lock()
	var expired []string
	now := time.Now()
	for hash, meta := range dc.index {
		if now.After(meta.ExpiresAt) {
			expired = append(expired, hash)
		}
	}
	for _, hash := range expired {
		meta := dc.index[hash]
		dc.currentSize.Add(-meta.Size)
		delete(dc.index, hash)
	}
	dc.mu.Unlock()

	for _, hash := range expired {
		os.Remove(dc.dataPath(hash))
		os.Remove(dc.metaPath(hash))
	}

	// LRU eviction if over 90% capacity
	threshold := int64(float64(dc.maxSize) * 0.9)
	if dc.currentSize.Load() <= threshold {
		return
	}

	target := int64(float64(dc.maxSize) * 0.8)
	dc.mu.Lock()
	// Collect all entries and sort by last access
	type item struct {
		hash string
		meta *diskMeta
	}
	var items []item
	for hash, meta := range dc.index {
		items = append(items, item{hash, meta})
	}
	dc.mu.Unlock()

	// Sort by last access (oldest first)
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].meta.LastAccess.After(items[j].meta.LastAccess) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	for _, it := range items {
		if dc.currentSize.Load() <= target {
			break
		}
		dc.mu.Lock()
		if meta, ok := dc.index[it.hash]; ok {
			dc.currentSize.Add(-meta.Size)
			delete(dc.index, it.hash)
		}
		dc.mu.Unlock()
		os.Remove(dc.dataPath(it.hash))
		os.Remove(dc.metaPath(it.hash))
	}
}
