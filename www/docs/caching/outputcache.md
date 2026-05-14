# Output Cache

`outputcache` caches HTTP responses with per-route opt-in. Routes that
don't opt in pass through; routes that do are served from storage on
cache hits and revalidated/refreshed per their configuration.

The package ships as three independent modules: a base module with the
cache logic and storage interface, and one module per storage backend.

## Install

For in-process caching:

```bash
go get github.com/joakimcarlsson/minmux/outputcache
go get github.com/joakimcarlsson/minmux/outputcache/inmemory
```

For Redis-backed caching:

```bash
go get github.com/joakimcarlsson/minmux/outputcache
go get github.com/joakimcarlsson/minmux/outputcache/redis
```

## Quick example

```go
package main

import (
    "net/http"
    "time"

    "github.com/joakimcarlsson/minmux/outputcache"
    "github.com/joakimcarlsson/minmux/outputcache/inmemory"
    "github.com/joakimcarlsson/minmux/router"
)

func main() {
    r := router.New()

    cache := outputcache.New(r, outputcache.Config{
        Storage:         inmemory.New(),
        DefaultDuration: time.Minute,
    })
    r.Use(cache.Middleware())

    r.Get("/products", listProducts,
        outputcache.WithOutputCache(time.Minute,
            outputcache.VaryByQuery("page", "sort"),
            outputcache.Tags("products"),
        ),
    )

    r.Post("/products", createProduct) // mutations invalidate the tag
    // After creating, call cache.InvalidateTag("products") in your handler.

    http.ListenAndServe(":8080", r)
}
```

Routes without `WithOutputCache` (or `WithCacheProfile`) are not cached.

## `Config`

`outputcache.New(router, Config)` accepts:

| Field | Purpose |
|---|---|
| `Storage` | The storage backend. Required. |
| `DefaultDuration` | TTL used when a route's `WithOutputCache` passes zero. Default 5 minutes. |
| `OnlyStatus` | Whitelist of cacheable response statuses. If unset, any 2xx is cached. |
| `ExcludeMethods` | Methods never cached. Default `POST`, `PUT`, `PATCH`, `DELETE`. |
| `CleanupInterval` | Background eviction interval used by backends that implement `StartCleanup`. Default 1 minute. |
| `Profiles` | A `*Profiles` registry for named cache configurations (see below). |

## Per-route opt-in

A route opts in to caching by attaching one of:

```go
outputcache.WithOutputCache(duration, opts...)  // inline config
outputcache.WithCacheProfile(name)              // named profile
```

Both are `router.Option`s and go alongside `openapi.Summary`, etc.

```go
r.Get("/products/{id}", getProduct,
    outputcache.WithOutputCache(time.Hour,
        outputcache.Tags("products", "product:single"),
    ),
)
```

## Vary functions

Vary the cache key by request attributes:

| Function | Effect on the cache key |
|---|---|
| `VaryByPath()` | Includes the path (default behaviour; explicit marker). |
| `VaryByQuery("limit", "page")` | Includes named query parameters, sorted by name and value. |
| `VaryByHeader("Accept-Language")` | Includes named request headers (canonicalised). |
| `VaryByEncoding()` | Shortcut for `VaryByHeader("Accept-Encoding")`. |
| `VaryByCustom(fn)` | Arbitrary function of `*http.Request` (e.g. user role, tenant). |

Keys are derived as a SHA-256 over `method | path | q:... | h:... | c:...`.

## Tags

Tag entries for group invalidation:

```go
r.Get("/products", listProducts,
    outputcache.WithOutputCache(time.Minute, outputcache.Tags("products")),
)
r.Get("/products/{id}", getProduct,
    outputcache.WithOutputCache(time.Hour,
        outputcache.Tags("products", "product:single"),
    ),
)

// In a mutation handler:
cache.InvalidateTag("products")
```

Tag bookkeeping is per-backend; both backends in this repo handle it.

## Sliding expiration

`SlidingExpiration()` extends an entry's TTL on every cache hit. Hot
entries effectively live forever; cold entries still expire after their
last access plus the configured TTL.

```go
outputcache.WithOutputCache(time.Minute,
    outputcache.VaryByPath(),
    outputcache.SlidingExpiration(),
)
```

## ETag revalidation

`WithRevalidation()` enables ETag-based 304 responses:

```go
outputcache.WithOutputCache(time.Minute, outputcache.WithRevalidation())
```

On cache hit the middleware emits an `ETag` derived from the body. A
follow-up request carrying `If-None-Match: <etag>` receives `304 Not
Modified` instead of the body.

## Conditional caching

`CacheWhen(fn)` filters which responses actually land in the cache:

```go
outputcache.WithOutputCache(time.Minute,
    outputcache.CacheWhen(func(status int, h http.Header) bool {
        return status == 200 && h.Get("X-No-Cache") == ""
    }),
)
```

## Profiles

Named, reusable cache configurations:

```go
profiles := outputcache.NewProfiles()
profiles.Add("hot", time.Hour,
    outputcache.VaryByPath(),
    outputcache.SlidingExpiration(),
    outputcache.Tags("hot"),
)

cache := outputcache.New(r, outputcache.Config{
    Storage:  inmemory.New(),
    Profiles: profiles,
})

r.Get("/products", listProducts, outputcache.WithCacheProfile("hot"))
```

`Profiles.Get(name)`, `Profiles.Remove(name)`, and `Profiles.List()` are
also available.

## Invalidation API

```go
cache.Clear()                            // remove every entry
cache.InvalidateTag("products")          // remove entries tagged "products"
cache.InvalidateTags("products", "hot")  // multi-tag invalidation
cache.Close()                            // stop the storage's cleanup goroutine
```

## Storage backends

### `outputcache/inmemory`

Process-local storage backed by `sync.RWMutex` + `map`. Includes a
periodic `cleanup` goroutine started by `Cache.New` when the storage
implements `StartCleanup`.

```go
import "github.com/joakimcarlsson/minmux/outputcache/inmemory"

store := inmemory.New()
```

Lost on process restart; no external dependencies.

### `outputcache/redis`

Redis-backed storage using `github.com/redis/go-redis/v9`. Entries are
JSON-encoded; tags are tracked in a Redis `SET` per tag so
`InvalidateTag` fans out via `SMEMBERS` + `DEL`.

```go
import (
    "github.com/joakimcarlsson/minmux/outputcache/redis"
    goredis "github.com/redis/go-redis/v9"
)

client := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
store := redis.New(redis.Options{
    Client: client,
    Prefix: "myapp:cache:", // optional; defaults to "minmux:outputcache:"
})
```

Survives process restart; shared across replicas.

## Writing a custom backend

Implement `outputcache.Storage`:

```go
type Storage interface {
    Get(key string) (*CachedResponse, bool)
    Set(key string, response *CachedResponse, ttl time.Duration)
    Delete(key string)
    Clear()
    InvalidateTag(tag string)
    InvalidateTags(tags ...string)
}
```

Optionally implement `StartCleanup(interval) func()` to receive a
periodic eviction tick from `Cache.New`.
