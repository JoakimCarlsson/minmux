package outputcache

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
)

// CacheKeyGenerator generates cache keys based on request attributes and
// holds the per-route options derived from the variadic opts passed to
// WithOutputCache / Profiles.Add.
type CacheKeyGenerator struct {
	varyByPath       bool
	varyByQuery      []string
	varyByHeaders    []string
	customFunc       func(*http.Request) string
	tags             []string
	cacheWhenFunc    func(int, http.Header) bool
	slidingExp       bool
	withRevalidation bool
}

// newCacheKeyGenerator builds a generator from an opaque list of option
// values. Each option is a func(*CacheKeyGenerator) returned by the
// VaryBy* / Tags / etc. helpers.
func newCacheKeyGenerator(opts []interface{}) *CacheKeyGenerator {
	gen := &CacheKeyGenerator{}
	for _, opt := range opts {
		if fn, ok := opt.(func(*CacheKeyGenerator)); ok {
			fn(gen)
		}
	}
	return gen
}

// GenerateKey returns the cache key for the given request.
func (g *CacheKeyGenerator) GenerateKey(r *http.Request) string {
	parts := []string{r.Method, r.URL.Path}

	if len(g.varyByQuery) > 0 {
		query := r.URL.Query()
		queryParts := make([]string, 0, len(g.varyByQuery))
		for _, param := range g.varyByQuery {
			if values := query[param]; len(values) > 0 {
				sortedValues := make([]string, len(values))
				copy(sortedValues, values)
				sort.Strings(sortedValues)
				queryParts = append(
					queryParts,
					param+"="+strings.Join(sortedValues, ","),
				)
			}
		}
		if len(queryParts) > 0 {
			sort.Strings(queryParts)
			parts = append(parts, "q:"+strings.Join(queryParts, "&"))
		}
	}

	if len(g.varyByHeaders) > 0 {
		headerParts := make([]string, 0, len(g.varyByHeaders))
		for _, header := range g.varyByHeaders {
			if value := r.Header.Get(header); value != "" {
				headerParts = append(headerParts, header+":"+value)
			}
		}
		if len(headerParts) > 0 {
			sort.Strings(headerParts)
			parts = append(parts, "h:"+strings.Join(headerParts, "|"))
		}
	}

	if g.customFunc != nil {
		if v := g.customFunc(r); v != "" {
			parts = append(parts, "c:"+v)
		}
	}

	combined := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// VaryByPath enables varying by path. minmux's default key already
// includes the path; this option exists for API parity with go-router.
func VaryByPath() interface{} {
	return func(g *CacheKeyGenerator) { g.varyByPath = true }
}

// VaryByQuery varies the cache by specific query parameters. Only the
// listed parameters affect the key; others are ignored.
func VaryByQuery(params ...string) interface{} {
	return func(g *CacheKeyGenerator) {
		g.varyByQuery = append(g.varyByQuery, params...)
	}
}

// VaryByHeader varies the cache by specific request headers. Header names
// are canonicalised via http.CanonicalHeaderKey.
func VaryByHeader(headers ...string) interface{} {
	return func(g *CacheKeyGenerator) {
		for _, h := range headers {
			g.varyByHeaders = append(
				g.varyByHeaders,
				http.CanonicalHeaderKey(h),
			)
		}
	}
}

// VaryByCustom varies the cache by an arbitrary function of the request.
// Useful for multi-tenant or role-based caching.
func VaryByCustom(fn func(*http.Request) string) interface{} {
	return func(g *CacheKeyGenerator) { g.customFunc = fn }
}

// Tags associates one or more tags with the cached response. Tags allow
// group invalidation via Cache.InvalidateTag / Cache.InvalidateTags.
func Tags(tags ...string) interface{} {
	return func(g *CacheKeyGenerator) { g.tags = append(g.tags, tags...) }
}

// SlidingExpiration extends the entry's TTL on each cache hit.
func SlidingExpiration() interface{} {
	return func(g *CacheKeyGenerator) { g.slidingExp = true }
}

// WithRevalidation enables ETag-based revalidation. Cached responses gain
// an ETag header; requests with a matching If-None-Match receive 304.
func WithRevalidation() interface{} {
	return func(g *CacheKeyGenerator) { g.withRevalidation = true }
}

// CacheWhen conditionally caches responses based on status and headers.
func CacheWhen(fn func(int, http.Header) bool) interface{} {
	return func(g *CacheKeyGenerator) { g.cacheWhenFunc = fn }
}

// VaryByEncoding varies the cache by Accept-Encoding. Use this when the
// upstream produces different bodies for gzip/br/etc.
func VaryByEncoding() interface{} {
	return VaryByHeader("Accept-Encoding")
}
