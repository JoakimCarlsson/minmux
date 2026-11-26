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
}

// Options configures a redis Store.
type Options struct {
	// Client is the Redis client to use. Required.
	Client redis.UniversalClient

	// Prefix is prepended to every cache key. Defaults to
	// "minmux:outputcache:" when empty.
	Prefix string
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
	return &Store{client: opts.Client, prefix: prefix}
}

func (s *Store) dataKey(key string) string { return s.prefix + key }
func (s *Store) tagKey(tag string) string  { return s.prefix + "tag:" + tag }

// Get retrieves a cached response, running its commands on ctx. Transient
// Redis errors and missing keys both produce a miss.
func (s *Store) Get(
	ctx context.Context,
	key string,
) (*outputcache.CachedResponse, bool) {
	raw, err := s.client.Get(ctx, s.dataKey(key)).Bytes()
	if err != nil {
		return nil, false
	}
	var e outputcache.CachedResponse
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, false
	}
	if e.SlidingExpiration {
		_ = s.client.Expire(ctx, s.dataKey(key), e.SlidingDuration).Err()
	}
	return &e, true
}

// Set stores a cached response with the given TTL, running its commands
// on ctx.
func (s *Store) Set(
	ctx context.Context,
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
	pipe.Set(ctx, s.dataKey(key), raw, ttl)
	for _, tag := range response.Tags {
		pipe.SAdd(ctx, s.tagKey(tag), key)
	}
	_, _ = pipe.Exec(ctx)
}

// Delete removes the cached response for key, running its command on ctx.
func (s *Store) Delete(ctx context.Context, key string) {
	_ = s.client.Del(ctx, s.dataKey(key)).Err()
}

// Clear removes every entry under this Store's prefix using SCAN + DEL,
// running its commands on ctx.
func (s *Store) Clear(ctx context.Context) {
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

// InvalidateTag removes every cached response carrying the given tag,
// running its commands on ctx.
func (s *Store) InvalidateTag(ctx context.Context, tag string) {
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

// InvalidateTags removes every cached response carrying any of the given
// tags, running its commands on ctx.
func (s *Store) InvalidateTags(ctx context.Context, tags ...string) {
	for _, t := range tags {
		s.InvalidateTag(ctx, t)
	}
}
