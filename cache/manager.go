package cache

// Manager coordinates the two-tier cache (memory + disk).
type Manager struct {
	memory *MemoryCache
	disk   *DiskCache
}

// NewManager creates a new cache manager.
func NewManager(memory *MemoryCache, disk *DiskCache) *Manager {
	return &Manager{memory: memory, disk: disk}
}

// Get looks up a cache key, checking memory first, then disk.
// On a disk hit, the entry is promoted to memory.
func (m *Manager) Get(key string) (*Entry, CacheStatus) {
	// Check memory first
	if entry, ok := m.memory.Get(key); ok {
		return entry, StatusHitMem
	}

	// Check disk
	if m.disk != nil {
		if entry, ok := m.disk.Get(key); ok {
			// Promote to memory
			_ = m.memory.Put(key, entry)
			return entry, StatusHitDisk
		}
	}

	return nil, StatusMiss
}

// Put stores an entry in both cache tiers as appropriate.
func (m *Manager) Put(key string, entry *Entry) {
	// Store in memory (MemoryCache.Put silently skips if too large)
	_ = m.memory.Put(key, entry)

	// Store on disk
	if m.disk != nil {
		_ = m.disk.Put(key, entry)
	}
}

// Delete removes an entry from both cache tiers.
func (m *Manager) Delete(key string) {
	m.memory.Delete(key)
	if m.disk != nil {
		m.disk.Delete(key)
	}
}

// Purge removes entries matching a prefix from both tiers.
func (m *Manager) Purge(prefix string) int {
	count := m.memory.Purge(prefix)
	if m.disk != nil {
		count += m.disk.Purge(prefix)
	}
	return count
}
