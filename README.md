# minmux

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](https://go.dev/)

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
- **Multi-module.** `router` is the runtime, `openapi` is the spec
  generator, `scalar` ships the docs UI. Pull only what you need.

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
Scalar UI — is in [`examples/scalar-ui`](examples/scalar-ui).

## Documentation

- [Quick Start](www/docs/getting-started/quick-start.md)
- [Feature Tour](www/docs/feature-tour.md)
- [Security](www/docs/security.md)
- [Streaming](www/docs/streaming.md)
- [Scalar UI](www/docs/scalar-ui.md)
- [Modules](www/docs/modules.md)

## Status

Early development. The API surface is still settling; expect breaking
changes until a v0.1.0 tag.

## License

MIT.
