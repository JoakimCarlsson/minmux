package outputcache

import (
	"net/http"
	"time"
)

// Config holds the configuration for the output cache.
type Config struct {
	// DefaultDuration is the default TTL for cached responses.
	// Individual routes can override this with WithOutputCache.
	DefaultDuration time.Duration

	// Storage is the cache storage backend to use.
	// Required — construct an inmemory.Store, redis.Store, or your own.
	Storage Storage

	// OnlyStatus specifies which HTTP status codes should be cached.
	// If nil, only 2xx status codes (200-299) are cached.
	OnlyStatus []int

	// ExcludeMethods specifies HTTP methods that should never be cached.
	// By default, POST/PUT/PATCH/DELETE are excluded.
	ExcludeMethods []string

	// CleanupInterval is the interval at which the storage cleanup hook
	// runs (used by in-memory backends with periodic eviction).
	// Default is 1 minute.
	CleanupInterval time.Duration

	// Profiles contains reusable cache configurations.
	Profiles *Profiles
}

// DefaultConfig returns a configuration with sensible defaults. Storage is
// nil and must be set by the caller before passing the Config to New.
func DefaultConfig() Config {
	return Config{
		DefaultDuration: 5 * time.Minute,
		ExcludeMethods: []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		},
		CleanupInterval: time.Minute,
	}
}

// shouldCache reports whether a request method is eligible for caching.
func (c *Config) shouldCache(method string) bool {
	for _, excluded := range c.ExcludeMethods {
		if method == excluded {
			return false
		}
	}
	return true
}

// shouldCacheStatus reports whether a response status is eligible for caching.
func (c *Config) shouldCacheStatus(status int) bool {
	if len(c.OnlyStatus) > 0 {
		for _, s := range c.OnlyStatus {
			if s == status {
				return true
			}
		}
		return false
	}
	return status >= 200 && status < 300
}
