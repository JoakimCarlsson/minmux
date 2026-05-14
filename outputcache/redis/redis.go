// Package redis provides a Redis-backed outputcache.Storage using
// github.com/redis/go-redis/v9. Entries are JSON-encoded; tags are
// tracked in a Redis SET per tag so InvalidateTag(s) can fan out via
// SMEMBERS + DEL.
package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/joakimcarlsson/minmux/outputcache"
	"github.com/redis/go-redis/v9"
)

// Store implements outputcache.Storage on Redis.
type Store struct {
	client redis.UniversalClient
	prefix string
	ctx    func() context.Context
}

// Options configures a redis Store.
type Options struct {
	// Client is the Redis client to use. Required.
	Client redis.UniversalClient

	// Prefix is prepended to every cache key. Defaults to
	// "minmux:outputcache:" when empty.
	Prefix string

	// Context returns the context used for all Redis operations. Defaults
	// to context.Background. Override if you want operations to inherit
	// a server-wide deadline.
	Context func() context.Context
}

// New constructs a redis Store. Panics if Options.Client is nil.
func New(opts Options) *Store {
	if opts.Client == nil {
		panic("outputcache/redis: Options.Client is required")
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = "minmux:outputcache:"
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background
	}
	return &Store{client: opts.Client, prefix: prefix, ctx: ctx}
}

func (s *Store) dataKey(key string) string { return s.prefix + key }
func (s *Store) tagKey(tag string) string  { return s.prefix + "tag:" + tag }

// Get retrieves a cached response. Transient Redis errors and missing
// keys both produce a miss.
func (s *Store) Get(key string) (*outputcache.CachedResponse, bool) {
	raw, err := s.client.Get(s.ctx(), s.dataKey(key)).Bytes()
	if err != nil {
		return nil, false
	}
	var e outputcache.CachedResponse
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, false
	}
	if e.SlidingExpiration {
		_ = s.client.Expire(s.ctx(), s.dataKey(key), e.SlidingDuration).Err()
	}
	return &e, true
}

// Set stores a cached response with the given TTL.
func (s *Store) Set(
	key string,
	response *outputcache.CachedResponse,
	ttl time.Duration,
) {
	response.ExpiresAt = response.CachedAt.Add(ttl)
	raw, err := json.Marshal(response)
	if err != nil {
		return
	}
	pipe := s.client.Pipeline()
	pipe.Set(s.ctx(), s.dataKey(key), raw, ttl)
	for _, tag := range response.Tags {
		pipe.SAdd(s.ctx(), s.tagKey(tag), key)
	}
	_, _ = pipe.Exec(s.ctx())
}

// Delete removes the cached response for key.
func (s *Store) Delete(key string) {
	_ = s.client.Del(s.ctx(), s.dataKey(key)).Err()
}

// Clear removes every entry under this Store's prefix using SCAN + DEL.
func (s *Store) Clear() {
	ctx := s.ctx()
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, s.prefix+"*", 256).
			Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = s.client.Del(ctx, keys...).Err()
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

// InvalidateTag removes every cached response carrying the given tag.
func (s *Store) InvalidateTag(tag string) {
	ctx := s.ctx()
	members, err := s.client.SMembers(ctx, s.tagKey(tag)).Result()
	if err != nil {
		return
	}
	pipe := s.client.Pipeline()
	for _, k := range members {
		pipe.Del(ctx, s.dataKey(k))
	}
	pipe.Del(ctx, s.tagKey(tag))
	_, _ = pipe.Exec(ctx)
}

// InvalidateTags removes every cached response carrying any of the given tags.
func (s *Store) InvalidateTags(tags ...string) {
	for _, t := range tags {
		s.InvalidateTag(t)
	}
}
