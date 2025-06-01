package mem_cache

import (
	"time"

	"github.com/patrickmn/go-cache"
)

var (
	// Private cache instance
	c *cache.Cache
)

// Initialize cache with custom settings
func InitCache(defaultExpiration, cleanupInterval time.Duration) {
	c = cache.New(defaultExpiration, cleanupInterval)
}

// Getter for cache (exported)
func GetCache() *cache.Cache {
	return c
}
