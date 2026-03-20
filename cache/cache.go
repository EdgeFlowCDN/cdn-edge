package cache

// CacheStatus represents the result of a cache lookup.
type CacheStatus string

const (
	StatusHitMem  CacheStatus = "HIT-MEM"
	StatusHitDisk CacheStatus = "HIT-DISK"
	StatusMiss    CacheStatus = "MISS"
)

// Cache is the interface for a cache backend.
type Cache interface {
	Get(key string) (*Entry, bool)
	Put(key string, entry *Entry) error
	Delete(key string)
	Purge(prefix string) int
}
