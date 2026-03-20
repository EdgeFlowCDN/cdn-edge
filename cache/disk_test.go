package cache

import (
	"os"
	"testing"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

func init() {
	// Initialize logger for tests
	_ = cdnlog.Init("error", "")
}

func TestDiskCachePutGet(t *testing.T) {
	dir, _ := os.MkdirTemp("", "disk-cache-test-*")
	defer os.RemoveAll(dir)

	dc, err := NewDiskCache(dir, 100*1024*1024, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Stop()

	// Wait for index build
	time.Sleep(100 * time.Millisecond)

	entry := newTestEntry("disk-key-1", 500)
	entry.Body = []byte("hello world from disk cache")
	entry.Size = int64(len(entry.Body))

	if err := dc.Put("disk-key-1", entry); err != nil {
		t.Fatal(err)
	}

	got, ok := dc.Get("disk-key-1")
	if !ok {
		t.Fatal("expected disk cache hit")
	}
	if string(got.Body) != "hello world from disk cache" {
		t.Errorf("body = %q, want %q", string(got.Body), "hello world from disk cache")
	}
}

func TestDiskCacheExpiry(t *testing.T) {
	dir, _ := os.MkdirTemp("", "disk-cache-test-*")
	defer os.RemoveAll(dir)

	dc, err := NewDiskCache(dir, 100*1024*1024, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Stop()
	time.Sleep(100 * time.Millisecond)

	entry := newTestEntry("expired-key", 100)
	entry.ExpiresAt = time.Now().Add(-1 * time.Second)
	dc.Put("expired-key", entry)

	_, ok := dc.Get("expired-key")
	if ok {
		t.Error("expired entry should not be returned")
	}
}

func TestDiskCacheDelete(t *testing.T) {
	dir, _ := os.MkdirTemp("", "disk-cache-test-*")
	defer os.RemoveAll(dir)

	dc, err := NewDiskCache(dir, 100*1024*1024, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Stop()
	time.Sleep(100 * time.Millisecond)

	entry := newTestEntry("del-key", 100)
	dc.Put("del-key", entry)
	dc.Delete("del-key")

	_, ok := dc.Get("del-key")
	if ok {
		t.Error("deleted entry should not be found")
	}
}

func TestDiskCacheObjectTooLarge(t *testing.T) {
	dir, _ := os.MkdirTemp("", "disk-cache-test-*")
	defer os.RemoveAll(dir)

	dc, err := NewDiskCache(dir, 100*1024*1024, 1024) // 1KB max object
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Stop()
	time.Sleep(100 * time.Millisecond)

	entry := newTestEntry("big-key", 2048)
	dc.Put("big-key", entry)

	_, ok := dc.Get("big-key")
	if ok {
		t.Error("oversized object should not be stored on disk")
	}
}
