# CORS

`github.com/joakimcarlsson/minmux/cors` is a CORS middleware that
operates on the standard `http.Handler` interface. It's independent of
the router — you can use it with any Go HTTP server — but plugs into
minmux via `Router.Use`.

## Install

```bash
go get github.com/joakimcarlsson/minmux/cors
```

Zero external dependencies.

## Development default

`cors.Default()` returns a permissive middleware: any origin, all
common methods, any request header, no credentials. Good for local
development, not for production.

```go
import (
    "github.com/joakimcarlsson/minmux/cors"
    "github.com/joakimcarlsson/minmux/router"
)

r := router.New()
r.Use(cors.Default())
```

## Production configuration

`cors.New(Options)` returns a middleware configured to your policy.

```go
r.Use(cors.New(cors.Options{
    AllowOrigins:     []string{"https://app.example.com", "*.staging.example.com"},
    AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch},
    AllowHeaders:     []string{"Authorization", "Content-Type"},
    ExposeHeaders:    []string{"X-Request-Id"},
    AllowCredentials: true,
    MaxAge:           3600,
}))
```

## Options

| Field | Purpose |
|---|---|
| `AllowOrigins` | List of allowed origins. Matched case-insensitively against the request `Origin` header. Supports `"*"` (any origin), exact match (`"https://app.com"`), and subdomain wildcards (`"*.example.com"`). |
| `AllowOriginFunc` | `func(origin string) bool`. When set, `AllowOrigins` is ignored — useful for dynamic allowlists (per-tenant, per-environment). |
| `AllowMethods` | Methods reported on preflight responses. Defaults to `GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD`. |
| `AllowHeaders` | Request headers reported as allowed on preflight responses. Use `["*"]` to allow any. |
| `ExposeHeaders` | Response headers the browser exposes to JavaScript. |
| `AllowCredentials` | Sets `Access-Control-Allow-Credentials: true`. |
| `MaxAge` | Seconds browsers may cache the preflight response. Zero omits the header. |

## Origin matching

The middleware checks each entry in `AllowOrigins` in order:

- `"*"` — matches every origin. **Incompatible with credentials per
  spec**: when `AllowCredentials` is also true, the middleware echoes
  the actual request origin back instead of `"*"` (browsers reject `"*"`
  with credentials).
- `"https://app.com"` — exact match (case-insensitive).
- `"*.example.com"` — subdomain wildcard. Matches `foo.example.com`,
  `a.b.example.com`, etc. Does **not** match the apex `example.com` —
  list it explicitly if you need it.

If no entry matches, the request proceeds to the handler **without
CORS headers**. The browser then enforces same-origin policy — the
fetch fails on the client side. The middleware does not reject the
request server-side.

## Requests without an Origin header

Non-CORS requests (same-origin browser navigations, server-to-server
calls, curl) carry no `Origin` header. The middleware passes them
through untouched — no CORS headers added, request reaches the handler.

## Vary headers

The middleware sets `Vary: Origin` on every CORS response so caches
don't serve a response for one origin to a request from another. On
preflights it also adds `Vary: Access-Control-Request-Method` and
`Vary: Access-Control-Request-Headers`.
