# minmux

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](https://go.dev/)

An HTTP framework for Go with first-class OpenAPI 3.2 generation. Built on
`net/http` so the entire `http.Handler` middleware ecosystem stays
compatible. Ships as independent modules — pull only what you need.

## Design

- **Context-style handlers.** Handlers are `func(c *router.Context)` or
  `func(c *router.Context, p Params)`. Write the response via `c.JSON`,
  `c.NoContent`, `c.Header`, etc. Familiar Go shape.
- **Typed parameter binding.** A Params struct with `path`, `query`,
  `header`, and `body` field tags is bound from the request. The framework
  reflects on the struct once at registration and produces a cached
  binder closure — no reflection per request.
- **Explicit OpenAPI annotations.** Describe each endpoint with
  `openapi.Summary`, `openapi.Tags`, `openapi.Returns`, and
  `openapi.ReturnsBody[T]`. Schemas for named struct types are hoisted into
  `components/schemas` and referenced via `$ref`. The spec is valid
  OpenAPI 3.2 with proper top-level field order.
- **First-class streaming.** SSE, JSONL, NDJSON, JSON-seq, and
  multipart/mixed all have matching `c.Stream` / `c.SSE` /
  `c.MultipartMixed` runtime helpers plus `openapi.StreamsBody[T]` /
  `openapi.SSEStream[T]` / `openapi.MultipartMixedStream[T]` options that
  emit OAS 3.2 `itemSchema` / `itemEncoding` / `prefixEncoding`. Streaming
  request bodies bind to `iter.Seq2[T, error]`. See
  [Streaming](streaming.md).
- **Multi-module.** `router` is the runtime, `openapi` is the spec
  generator. Independent versioning; the router has zero dependencies on
  the openapi package.
- **Built on `net/http`.** Group routing uses Go 1.22+ ServeMux pattern
  matching. Standard middleware (`func(http.Handler) http.Handler`) plugs
  in directly via `Use`.

## Tiny example

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

A working multi-endpoint example lives in `examples/todo/` — see the
[Quick Start](getting-started/quick-start.md) for a walkthrough.

## Status

Early development. The API surface is still settling; expect breaking
changes until a v0.1.0 tag.
