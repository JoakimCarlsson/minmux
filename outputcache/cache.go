package outputcache

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

// Cache manages HTTP response caching with configurable storage and policies.
// Mirrors github.com/joakimcarlsson/go-router/outputcache.Cache.
type Cache struct {
	config   Config
	storage  Storage
	router   *router.Router
	profiles *Profiles
	cleanup  func()
}

// New creates a Cache bound to a router. The router is used to look up
// per-route cache configuration (set via WithOutputCache or WithCacheProfile)
// at request time. Panics if config.Storage is nil.
func New(r *router.Router, config Config) *Cache {
	if config.Storage == nil {
		panic("outputcache: Config.Storage is required")
	}
	if config.DefaultDuration == 0 {
		config.DefaultDuration = 5 * time.Minute
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = time.Minute
	}
	if len(config.ExcludeMethods) == 0 {
		config.ExcludeMethods = []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		}
	}

	cache := &Cache{
		config:   config,
		storage:  config.Storage,
		router:   r,
		profiles: config.Profiles,
	}

	if c, ok := config.Storage.(cleanupStarter); ok {
		cache.cleanup = c.StartCleanup(config.CleanupInterval)
	}

	return cache
}

// cleanupStarter is implemented by storage backends that need a periodic
// eviction hook (notably the in-memory backend).
type cleanupStarter interface {
	StartCleanup(interval time.Duration) func()
}

// Middleware returns an HTTP middleware that handles output caching for
// routes that have opted in via WithOutputCache or WithCacheProfile.
// Routes without cache configuration pass through unmodified.
func (c *Cache) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !c.config.shouldCache(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			ep := c.router.Match(r)
			cfg := routeConfigOf(ep)
			if cfg == nil {
				next.ServeHTTP(w, r)
				return
			}

			duration := cfg.Duration
			opts := cfg.Options
			if cfg.Profile != "" && c.profiles != nil {
				if p := c.profiles.Get(cfg.Profile); p != nil {
					duration = p.Duration
					opts = p.Options
				}
			}

			keyGen := newCacheKeyGenerator(opts)
			cacheKey := keyGen.GenerateKey(r)

			if cached, ok := c.storage.Get(cacheKey); ok {
				if keyGen.withRevalidation && cached.ETag != "" &&
					shouldRevalidate(
						r.Header.Get("If-None-Match"),
						cached.ETag,
					) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				c.serveCachedResponse(w, cached)
				return
			}

			recorder := &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				body:           &bytes.Buffer{},
			}
			next.ServeHTTP(recorder, r)

			if keyGen.cacheWhenFunc != nil &&
				!keyGen.cacheWhenFunc(recorder.statusCode, recorder.Header()) {
				return
			}
			if !c.config.shouldCacheStatus(recorder.statusCode) {
				return
			}

			if duration == 0 {
				duration = c.config.DefaultDuration
			}

			body := bytes.Clone(recorder.body.Bytes())
			etag := ""
			if keyGen.withRevalidation {
				etag = generateETag(body)
				w.Header().Set("ETag", etag)
			}

			now := time.Now()
			c.storage.Set(cacheKey, &CachedResponse{
				StatusCode:        recorder.statusCode,
				Headers:           recorder.Header().Clone(),
				Body:              body,
				CachedAt:          now,
				ExpiresAt:         now.Add(duration),
				Tags:              keyGen.tags,
				SlidingExpiration: keyGen.slidingExp,
				SlidingDuration:   duration,
				ETag:              etag,
			}, duration)
		})
	}
}

// serveCachedResponse writes a cached response to the ResponseWriter.
func (c *Cache) serveCachedResponse(
	w http.ResponseWriter,
	cached *CachedResponse,
) {
	for key, values := range cached.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(cached.StatusCode)
	_, _ = w.Write(cached.Body)
}

// Clear removes every cached response.
func (c *Cache) Clear() { c.storage.Clear() }

// InvalidateTag removes every cached response carrying the given tag.
func (c *Cache) InvalidateTag(tag string) { c.storage.InvalidateTag(tag) }

// InvalidateTags removes every cached response carrying any of the given tags.
func (c *Cache) InvalidateTags(
	tags ...string,
) {
	c.storage.InvalidateTags(tags...)
}

// Close stops any background cleanup hook started on the storage backend.
func (c *Cache) Close() {
	if c.cleanup != nil {
		c.cleanup()
	}
}

// responseRecorder captures the upstream response so it can be cached
// while still streaming to the client.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	buf := &bytes.Buffer{}
	n, err := io.Copy(io.MultiWriter(r.ResponseWriter, buf), src)
	r.body.Write(buf.Bytes())
	return n, err
}
