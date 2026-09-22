# minmux

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)

An opinionated HTTP framework for Go with first-class OpenAPI 3.2 generation.
Inspired by [ASP.NET Core Minimal APIs](https://learn.microsoft.com/aspnet/core/fundamentals/minimal-apis):
the handler signature is the contract — parameters bind from the request,
results serialize to the response, and the same shape drives spec generation.

Successor to [`JoakimCarlsson/go-router`](https://github.com/JoakimCarlsson/go-router).
minmux is a rewrite around Go 1.22+ `ServeMux` pattern routing with a
sharper, more opinionated API surface and full OpenAPI 3.2 support.

## Why opinionated

- **One way to bind parameters.** A `Params` struct with `path`,
  `query`, `header`, `body`, `form`, and `file` tags. The framework
  reflects once at registration and caches a binder closure — no
  reflection per request.
- **OpenAPI from the same source.** No duplicate type declarations:
  the spec is derived from the handler signature plus a small set of
  annotations (`openapi.Summary`, `openapi.Tags`,
  `openapi.ReturnsBody[T]`, ...).
- **Streaming as a first-class concept.** SSE, JSONL, NDJSON, JSON-seq,
  and `multipart/mixed` all have matching runtime helpers
  (`c.SSE`, `c.Stream`, `c.MultipartMixed`) and OpenAPI options
  (`StreamsBody[T]`, `SSEStream[T]`, `MultipartMixedStream[T]`) that
  emit the OAS 3.2 `itemSchema` / `itemEncoding` / `prefixEncoding`
  fields. Request bodies bind to `iter.Seq2[T, error]`.
- **Declarative security.** `apiKey`, HTTP Basic / Bearer, `mutualTLS`,
  OAuth2 (including the OAS 3.2 `deviceAuthorization` flow), and
  OpenID Connect, with document-level defaults and per-route overrides
  (`Security`, `SecurityAll`, `OptionalSecurity`, `NoSecurity`).
- **Typed clients from the same source.** `clientgen` turns the derived
  spec into a Go client — one method per operation, typed params and
  results, `*APIError` carrying the decoded error body, and
  `iter.Seq2[T, error]` for streams. Output is deterministic, so a
  committed client plus a regenerate-and-diff step turns a breaking API
  change into a compile error in the caller.
- **Multi-module.** `router` is the runtime, `openapi` is the spec
  generator, `clientgen` emits typed clients, `scalar` ships the docs UI.
  Pull only what you need.

## Hello world

```go
package main

import (
    "log"
    "net/http"

    "github.com/joakimcarlsson/minmux/openapi"
    "github.com/joakimcarlsson/minmux/router"
)

type GetUserParams struct {
    ID int `path:"id"`
}

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    r := router.New()

    r.Get("/users/{id}", func(c *router.Context, p GetUserParams) {
        c.JSON(http.StatusOK, User{ID: p.ID, Name: "Joe"})
    },
        openapi.Summary("Get user by ID"),
        openapi.Tags("Users"),
        openapi.ReturnsBody[User](http.StatusOK, "User found"),
    )

    gen := openapi.NewGenerator(openapi.Info{Title: "API", Version: "0.1.0"})
    r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

    log.Fatal(http.ListenAndServe(":8080", r))
}
```

A working multi-endpoint example lives in [`examples/todo`](examples/todo).
A larger surface — every security scheme, streaming response, and the
Scalar UI — is in [`examples/scalar-ui`](examples/scalar-ui). The full
generated-client loop, from server to committed client to round-trip
test, is in [`examples/clientgen`](examples/clientgen).

## Documentation

- [Quick Start](www/docs/getting-started/quick-start.md)
- [Feature Tour](www/docs/feature-tour.md)
- [Security](www/docs/security.md)
- [Streaming](www/docs/streaming.md)
- [Scalar UI](www/docs/scalar-ui.md)
- [Client Generation](www/docs/clientgen.md)
- [Modules](www/docs/modules.md)

## Status

The first public release is v0.1.0. The API surface is still settling, so
minor releases may include breaking changes until v1.0.0.

## Versioning

Each module is versioned independently with a path-prefixed git tag. The tag
prefix must match the module directory exactly so the Go module system can
resolve it.

| Module | Tag format | Example |
|--------|------------|---------|
| router | `router/vX.Y.Z` | `router/v0.1.0` |
| openapi | `openapi/vX.Y.Z` | `openapi/v0.1.0` |
| outputcache/inmemory | `outputcache/inmemory/vX.Y.Z` | `outputcache/inmemory/v0.1.0` |

All modules follow [semantic versioning](https://semver.org).

## Release process

Releases follow the same model as the AI repository: CI on `main` is the
safety net, module tags drive `go get` resolution, and dated GitHub Releases
collect the module releases into a changelog.

### 1. Ensure main is green

CI must pass on the latest commit before tagging.

### 2. Sync internal dependencies

When a shared module changes, update the version required by each module that
publishes it to users. The helper detects and fixes requirements that lag the
latest published module tag:

```bash
scripts/sync-deps.sh --check
scripts/sync-deps.sh
```

Commit and merge any resulting `go.mod` and `go.sum` changes before tagging
the affected consumers. Skip this step for an isolated change that cannot
affect another module's public build.

### 3. Tag changed modules

```bash
# List every workspace module
scripts/release.sh modules

# Create a local tag for inspection
scripts/release.sh tag -m router -v v0.1.0

# Create and push the tag
make release-tag MODULE=router VERSION=v0.1.0
```

The script verifies that the module exists, the tag uses semantic versioning,
the working tree is clean, and the release is made from `main`.

### 4. Warm the Go module proxy

```bash
scripts/release.sh warm -t router/v0.1.0
```

Run this for every new module tag so it is immediately available through
`go get`.

### 5. Create a dated GitHub Release

```bash
# Preview the release notes
scripts/release.sh release

# Publish release-YYYY-MM-DD and its GitHub Release
make release PUBLISH=1
```

The release lists every module tag created since the previous dated release.

## License

MIT.
