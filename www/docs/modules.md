# Modules

minmux is published as independent Go modules. Each lives at
`github.com/joakimcarlsson/minmux/<path>` and is versioned independently
with path-prefixed tags (e.g. `router/v0.1.0`).

You install only the modules you use.

## Core

| Module | Purpose |
|---|---|
| `router` | Routing, parameter binding, response context helpers, ProblemDetails. Built on `net/http`. Zero external dependencies. |

## Documentation

| Module | Purpose |
|---|---|
| `openapi` | OpenAPI 3.1 spec generation from explicit endpoint annotations. Depends on `router`. |

## Planned

Modules slated for follow-up releases:

- `outputcache` — HTTP response caching with pluggable storage and
  flexible cache key strategies.
- `scalar` — Scalar docs UI for serving the generated OpenAPI spec.
- Additional middleware and integrations (CORS, auth, rate limiting)
  may stay inside `router` or graduate to their own modules as they
  grow.
