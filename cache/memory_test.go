package cache

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func newTestEntry(key string, size int64) *Entry {
	return &Entry{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       make([]byte, size),
		Size:       size,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
		Key:        key,
		Hash:       HashKey(key),
	}
}

func TestMemoryCachePutGet(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1*1024*1024) // 10MB total, 1MB per object

	entry := newTestEntry("key1", 1000)
	if err := mc.Put("key1", entry); err != nil {
		t.Fatal(err)
	}

	got, ok := mc.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

func TestMemoryCacheExpiry(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1*1024*1024)

	entry := newTestEntry("key1", 100)
	entry.ExpiresAt = time.Now().Add(-1 * time.Second) // already expired
	mc.Put("key1", entry)

	_, ok := mc.Get("key1")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestMemoryCacheEviction(t *testing.T) {
	mc := NewMemoryCache(1024, 512) // very small: 1KB total, 512B per object

	// Fill cache
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		mc.Put(key, newTestEntry(key, 200))
	}

	// Older entries should have been evicted
	// At least the most recent entries should be present
	_, ok := mc.Get("key9")
	if !ok {
		t.Error("most recent entry should still be in cache")
	}
}

func TestMemoryCacheObjectTooLarge(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1024) // 1KB max object

	entry := newTestEntry("key1", 2048) // 2KB, exceeds limit
	mc.Put("key1", entry)

	_, ok := mc.Get("key1")
	if ok {
		t.Error("oversized object should not be cached in memory")
	}
}

func TestMemoryCacheDelete(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1*1024*1024)

	mc.Put("key1", newTestEntry("key1", 100))
	mc.Delete("key1")

	_, ok := mc.Get("key1")
	if ok {
		t.Error("deleted entry should not be found")
	}
}

func TestMemoryCacheConcurrent(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1*1024*1024)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			mc.Put(key, newTestEntry(key, 100))
			mc.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestMemoryCachePurge(t *testing.T) {
	mc := NewMemoryCache(10*1024*1024, 1*1024*1024)

	mc.Put("http://cdn.example.com/a/1", newTestEntry("http://cdn.example.com/a/1", 100))
	mc.Put("http://cdn.example.com/a/2", newTestEntry("http://cdn.example.com/a/2", 100))
	mc.Put("http://cdn.example.com/b/1", newTestEntry("http://cdn.example.com/b/1", 100))

	count := mc.Purge("http://cdn.example.com/a/")
	if count != 2 {
		t.Errorf("Purge count = %d, want 2", count)
	}

	_, ok := mc.Get("http://cdn.example.com/b/1")
	if !ok {
		t.Error("non-matching entry should still exist")
	}
}

func BenchmarkMemoryCacheGet(b *testing.B) {
	mc := NewMemoryCache(100*1024*1024, 1*1024*1024)
	entry := newTestEntry("bench-key", 1000)
	mc.Put("bench-key", entry)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.Get("bench-key")
		}
	})
}

func BenchmarkMemoryCachePut(b *testing.B) {
	mc := NewMemoryCache(100*1024*1024, 1*1024*1024)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i)
			mc.Put(key, newTestEntry(key, 1000))
			i++
		}
	})
}
