package cache

import (
	"container/list"
	"hash/fnv"
	"sync"
)

const defaultShardCount = 256

// MemoryCache is a sharded LRU in-memory cache.
type MemoryCache struct {
	shards        []*memoryShard
	shardCount    int
	maxSize       int64 // total max bytes
	maxObjectSize int64 // max single object bytes
}

type memoryShard struct {
	mu       sync.Mutex
	items    map[string]*list.Element
	evictList *list.List
	size     int64
	maxSize  int64
}

type memoryItem struct {
	key   string
	entry *Entry
}

// NewMemoryCache creates a new sharded LRU memory cache.
func NewMemoryCache(maxSize, maxObjectSize int64) *MemoryCache {
	shardMax := maxSize / int64(defaultShardCount)
	if shardMax < 1 {
		shardMax = 1
	}
	shards := make([]*memoryShard, defaultShardCount)
	for i := range shards {
		shards[i] = &memoryShard{
			items:     make(map[string]*list.Element),
			evictList: list.New(),
			maxSize:   shardMax,
		}
	}
	return &MemoryCache{
		shards:        shards,
		shardCount:    defaultShardCount,
		maxSize:       maxSize,
		maxObjectSize: maxObjectSize,
	}
}

func (mc *MemoryCache) getShard(key string) *memoryShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return mc.shards[h.Sum32()%uint32(mc.shardCount)]
}

func (mc *MemoryCache) Get(key string) (*Entry, bool) {
	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, ok := shard.items[key]; ok {
		entry := elem.Value.(*memoryItem).entry
		if entry.IsExpired() {
			shard.removeElement(elem)
			return nil, false
		}
		shard.evictList.MoveToFront(elem)
		return entry, true
	}
	return nil, false
}

func (mc *MemoryCache) Put(key string, entry *Entry) error {
	if entry.Size > mc.maxObjectSize {
		return nil // too large for memory cache, skip silently
	}

	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Update existing entry
	if elem, ok := shard.items[key]; ok {
		shard.size -= elem.Value.(*memoryItem).entry.Size
		elem.Value.(*memoryItem).entry = entry
		shard.size += entry.Size
		shard.evictList.MoveToFront(elem)
	} else {
		// Evict until there's room
		for shard.size+entry.Size > shard.maxSize && shard.evictList.Len() > 0 {
			shard.evictOldest()
		}
		item := &memoryItem{key: key, entry: entry}
		elem := shard.evictList.PushFront(item)
		shard.items[key] = elem
		shard.size += entry.Size
	}
	return nil
}

func (mc *MemoryCache) Delete(key string) {
	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, ok := shard.items[key]; ok {
		shard.removeElement(elem)
	}
}

func (mc *MemoryCache) Purge(prefix string) int {
	count := 0
	for _, shard := range mc.shards {
		shard.mu.Lock()
		var toDelete []*list.Element
		for key, elem := range shard.items {
			if len(prefix) == 0 || key == prefix || (len(key) > len(prefix) && key[:len(prefix)] == prefix) {
				toDelete = append(toDelete, elem)
			}
		}
		for _, elem := range toDelete {
			shard.removeElement(elem)
			count++
		}
		shard.mu.Unlock()
	}
	return count
}

func (s *memoryShard) evictOldest() {
	elem := s.evictList.Back()
	if elem != nil {
		s.removeElement(elem)
	}
}

func (s *memoryShard) removeElement(elem *list.Element) {
	item := elem.Value.(*memoryItem)
	s.evictList.Remove(elem)
	delete(s.items, item.key)
	s.size -= item.entry.Size
}
