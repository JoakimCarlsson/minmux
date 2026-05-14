// Package inmemory provides an in-process outputcache.Storage backed by a
// concurrent map with periodic expiration cleanup and a tag index for
// group invalidation. Mirrors go-router/outputcache.MemoryStorage.
package inmemory

import (
	"sync"
	"time"

	"github.com/joakimcarlsson/minmux/outputcache"
)

// Store implements outputcache.Storage in process memory.
type Store struct {
	mu       sync.RWMutex
	cache    map[string]*outputcache.CachedResponse
	tagIndex map[string][]string
}

// New constructs an in-memory Store.
func New() *Store {
	return &Store{
		cache:    make(map[string]*outputcache.CachedResponse),
		tagIndex: make(map[string][]string),
	}
}

// Get retrieves a cached response by key. Expired entries are removed
// lazily; entries with sliding expiration have their ExpiresAt extended.
func (s *Store) Get(key string) (*outputcache.CachedResponse, bool) {
	s.mu.RLock()
	response, ok := s.cache[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}

	now := time.Now()
	if now.After(response.ExpiresAt) {
		s.Delete(key)
		return nil, false
	}

	if response.SlidingExpiration {
		s.mu.Lock()
		response.ExpiresAt = now.Add(response.SlidingDuration)
		s.mu.Unlock()
	}

	return response, true
}

// Set stores a cached response with the given TTL.
func (s *Store) Set(
	key string,
	response *outputcache.CachedResponse,
	ttl time.Duration,
) {
	response.ExpiresAt = response.CachedAt.Add(ttl)

	s.mu.Lock()
	s.cache[key] = response
	for _, tag := range response.Tags {
		s.tagIndex[tag] = append(s.tagIndex[tag], key)
	}
	s.mu.Unlock()
}

// Delete removes the cached response for key, cleaning the tag index.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	response, ok := s.cache[key]
	if !ok {
		return
	}
	for _, tag := range response.Tags {
		keys := s.tagIndex[tag]
		for i, k := range keys {
			if k == key {
				s.tagIndex[tag] = append(keys[:i], keys[i+1:]...)
				break
			}
		}
		if len(s.tagIndex[tag]) == 0 {
			delete(s.tagIndex, tag)
		}
	}
	delete(s.cache, key)
}

// Clear removes every cached response.
func (s *Store) Clear() {
	s.mu.Lock()
	s.cache = make(map[string]*outputcache.CachedResponse)
	s.tagIndex = make(map[string][]string)
	s.mu.Unlock()
}

// InvalidateTag removes every cached response carrying the given tag.
func (s *Store) InvalidateTag(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys, ok := s.tagIndex[tag]
	if !ok {
		return
	}
	for _, key := range keys {
		delete(s.cache, key)
	}
	delete(s.tagIndex, tag)
}

// InvalidateTags removes every cached response carrying any of the given tags.
func (s *Store) InvalidateTags(tags ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keysToDelete := make(map[string]bool)
	for _, tag := range tags {
		if keys, ok := s.tagIndex[tag]; ok {
			for _, key := range keys {
				keysToDelete[key] = true
			}
			delete(s.tagIndex, tag)
		}
	}
	for key := range keysToDelete {
		delete(s.cache, key)
	}
}

// StartCleanup runs a background goroutine that periodically removes
// expired entries. Returns a function that stops the goroutine.
func (s *Store) StartCleanup(interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanup()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func (s *Store) cleanup() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, response := range s.cache {
		if now.After(response.ExpiresAt) {
			delete(s.cache, key)
		}
	}
}
