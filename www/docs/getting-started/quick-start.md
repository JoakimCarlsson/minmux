# Quick Start

Build and run your first minmux endpoint in five minutes. This page
shows just enough to get a working server with one typed handler and a
served OpenAPI spec. For everything else — middleware, forms, uploads,
schema constraints, response headers, structured tags, deprecation,
operationId — see the [Feature Tour](../feature-tour.md).

## A minimal server

Create a new module and pull the two packages:

```bash
go mod init example.com/api
go get github.com/joakimcarlsson/minmux/router
go get github.com/joakimcarlsson/minmux/openapi
```

Then `main.go`:

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
    r.Use(router.Recover())

    r.Get("/users/{id}", func(c *router.Context, p GetUserParams) {
        c.JSON(http.StatusOK, User{ID: p.ID, Name: "Joe"})
    },
        openapi.Summary("Get user by ID"),
        openapi.Tags("Users"),
        openapi.ReturnsBody[User](http.StatusOK, "User found"),
    )

    gen := openapi.NewGenerator(openapi.Info{Title: "Users API", Version: "0.1.0"})
    r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

    log.Fatal(http.ListenAndServe(":8080", r))
}
```

Run it:

```bash
go run .
```

```bash
curl http://localhost:8080/users/42
# {"id":42,"name":"Joe"}

curl http://localhost:8080/openapi.json | jq .paths
```

## What's happening

- **`router.New()`** returns an `http.Handler` built on `net/http`.
- **`router.Recover()`** turns panics into 500 ProblemDetails responses.
- **`GetUserParams`** is a typed Params struct. The `path:"id"` tag binds
  the `{id}` segment and parses it as `int`. A non-integer like
  `/users/abc` returns a 400 ProblemDetails automatically.
- **`openapi.Summary` / `Tags` / `ReturnsBody[User]`** are
  registration-time options that describe the endpoint in the generated
  spec. Schemas for named types (`User`) are hoisted into
  `components/schemas` and referenced via `$ref`.
- **`gen.Handler(r)`** returns an `http.HandlerFunc` that serves the
  OpenAPI 3.2 document.

## Next steps

- **[Feature Tour](../feature-tour.md)** — the full surface: query and
  header params, request bodies, forms and uploads, schema constraints,
  middleware, response headers, structured tags, error responses.
- **[Streaming](../streaming.md)** — SSE, JSONL, NDJSON, JSON-seq, and
  multipart/mixed responses and request bodies.
- **[Security](../security.md)** — `apiKey`, HTTP Basic / Bearer,
  `mutualTLS`, OAuth2, OpenID Connect declarations.
- **[Scalar UI](../scalar-ui.md)** — drop-in interactive reference at
  `/docs`.
- **`examples/todo/`** in the repo — a runnable multi-endpoint API
  exercising path / query / body params, group cascading, 200 / 201 /
  204 / 400 / 404 responses, and a served spec.
