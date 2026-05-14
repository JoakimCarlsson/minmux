# Modules

minmux is published as independent Go modules. Each lives at
`github.com/joakimcarlsson/minmux/<path>` and is versioned independently
with path-prefixed tags (e.g. `router/v0.1.0`).

You install only the modules you use.

## Core

| Module | Purpose |
|---|---|
| `router` | Routing, parameter binding (path / query / header / form / file / body), response context helpers, ProblemDetails, multipart and raw-stream uploads, plus first-class streaming response writers (`c.SSE`, `c.Stream`, `c.MultipartMixed`) and `iter.Seq2[T, error]` body binders for SSE / JSONL / JSON-seq / multipart/mixed inputs. Built on `net/http`. Zero external dependencies. |

## Documentation

| Module | Purpose |
|---|---|
| `openapi` | OpenAPI 3.2 spec generation from explicit endpoint annotations, with auto-mapped numeric formats (int32/int64/float/double), a `format:"..."` struct tag for string formats (email, password, uuid, ...), streaming options (`StreamsBody[T]`, `SSEStream[T]`, `MultipartMixedStream[T]`) that emit `itemSchema` / `itemEncoding` / `prefixEncoding`, the full OAS 3.2 security model (`apiKey`, `http`, `mutualTLS`, `oauth2` incl. `deviceAuthorization`, `openIdConnect`) with declarative document-level and per-endpoint requirements, and document-level `servers` (with templated URL variables) for base-URL selection. Depends on `router`. |
| `scalar` | Serves the [Scalar API Reference](https://scalar.com) UI as an `http.HandlerFunc` pointed at an OpenAPI document URL — typically the same-origin path served by `openapi.Generator.Handler`. Renders OAS 3.x including the 3.2 fields minmux emits. Zero external dependencies. See [Scalar UI](scalar-ui.md). |

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
