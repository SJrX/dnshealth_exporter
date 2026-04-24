package cache

import (
	"sync"
	"time"
)

// DelegationEntry stores a cached delegation result with its timestamp.
type DelegationEntry struct {
	// Data holds the cached delegation result. The concrete type is
	// *prober.DelegationResult but stored as any to avoid circular imports.
	Data     any
	CachedAt time.Time
}

// DelegationCache caches DNS delegation walk results with a TTL.
// Only non-target infrastructure (root, TLD, parent servers) is cached.
// Authoritative nameserver queries are never cached.
type DelegationCache struct {
	mu      sync.RWMutex
	entries map[string]*DelegationEntry
	ttl     time.Duration
}

// NewDelegationCache creates a new delegation cache with the given TTL.
func NewDelegationCache(ttl time.Duration) *DelegationCache {
	return &DelegationCache{
		entries: make(map[string]*DelegationEntry),
		ttl:     ttl,
	}
}

// Get retrieves a cached delegation result for the given zone.
// Returns nil if the entry doesn't exist or has expired.
func (c *DelegationCache) Get(zone string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[zone]
	if !ok {
		return nil
	}
	if time.Since(entry.CachedAt) > c.ttl {
		return nil
	}
	return entry.Data
}

// Set stores a delegation result for the given zone.
func (c *DelegationCache) Set(zone string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[zone] = &DelegationEntry{
		Data:     data,
		CachedAt: time.Now(),
	}
}

// Invalidate clears all cached entries. Called on config reload.
func (c *DelegationCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*DelegationEntry)
}

// Len returns the number of entries in the cache (for testing).
func (c *DelegationCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
