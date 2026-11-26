package outputcache

import (
	"context"
	"net/http"
	"time"
)

// CachedResponse is one cached HTTP response with metadata.
type CachedResponse struct {
	StatusCode        int           `json:"status_code"`
	Headers           http.Header   `json:"headers"`
	Body              []byte        `json:"body"`
	CachedAt          time.Time     `json:"cached_at"`
	ExpiresAt         time.Time     `json:"expires_at"`
	Tags              []string      `json:"tags,omitempty"`
	SlidingExpiration bool          `json:"sliding_expiration,omitempty"`
	SlidingDuration   time.Duration `json:"sliding_duration,omitempty"`
	ETag              string        `json:"etag,omitempty"`
}

// Storage is the persistence backend for cached responses. Implementations
// must be safe for concurrent use. Every method takes the context of the
// request or caller it serves, so a backend that does I/O honours its
// cancellation and deadline and its traced client calls join its trace.
type Storage interface {
	// Get retrieves a cached response by key.
	// Returns the cached response and true if found, nil and false otherwise.
	// Implementations are expected to handle expiration and sliding TTL
	// internally.
	Get(ctx context.Context, key string) (*CachedResponse, bool)

	// Set stores a cached response with the given TTL.
	Set(
		ctx context.Context,
		key string,
		response *CachedResponse,
		ttl time.Duration,
	)

	// Delete removes a cached response by key.
	Delete(ctx context.Context, key string)

	// Clear removes all cached responses.
	Clear(ctx context.Context)

	// InvalidateTag removes all cached responses with the given tag.
	InvalidateTag(ctx context.Context, tag string)

	// InvalidateTags removes all cached responses with any of the given tags.
	InvalidateTags(ctx context.Context, tags ...string)
}
