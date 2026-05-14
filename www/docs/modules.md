# Modules

minmux is published as independent Go modules. Each lives at
`github.com/joakimcarlsson/minmux/<path>` and is versioned independently
with path-prefixed tags (e.g. `router/v0.1.0`).

You install only the modules you use.

## Core

| Module | Purpose |
|---|---|
| `router` | Routing, parameter binding (path / query / header / form / file / body), response context helpers, ProblemDetails, multipart and raw-stream uploads. Built on `net/http`. Zero external dependencies. |

## Documentation

| Module | Purpose |
|---|---|
| `openapi` | OpenAPI 3.2 spec generation from explicit endpoint annotations, with auto-mapped numeric formats (int32/int64/float/double) and a `format:"..."` struct tag for string formats (email, password, uuid, ...). Depends on `router`. |

## Middleware

| Module | Purpose |
|---|---|
| `cors` | CORS middleware compatible with any `http.Handler`. Zero external dependencies. |

## Caching

| Module | Purpose |
|---|---|
| `outputcache` | HTTP response caching with per-route opt-in, tags, sliding expiration, ETag revalidation, profiles. Depends on `router`. |
| `outputcache/inmemory` | In-process `outputcache.Storage` backed by a concurrent map. Depends on `outputcache`. |
| `outputcache/redis` | Redis-backed `outputcache.Storage` using `redis/go-redis/v9`. Depends on `outputcache`. |
