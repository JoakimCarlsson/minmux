# Quick Start

This walkthrough builds a minimal Todo API end-to-end: routing, typed
parameter binding, JSON responses, error cases, and a served OpenAPI 3.2
spec. The complete source lives in `examples/todo/`.

## Hello world

The smallest possible minmux server:

```go
package main

import (
    "log"
    "net/http"

    "github.com/joakimcarlsson/minmux/router"
)

func main() {
    r := router.New()

    r.Get("/hello", func(c *router.Context) {
        c.JSON(http.StatusOK, map[string]string{"message": "hello"})
    })

    log.Fatal(http.ListenAndServe(":8080", r))
}
```

`router.Router` implements `http.Handler`, so it plugs into
`http.ListenAndServe`, `http.Server{}`, `httptest.NewServer`, or any
other consumer of an `http.Handler`.

## Path parameters

A Params struct with `path` field tags drives the binding:

```go
type GetUserParams struct {
    ID int `path:"id"`
}

r.Get("/users/{id}", func(c *router.Context, p GetUserParams) {
    c.JSON(http.StatusOK, map[string]any{"id": p.ID})
})
```

Sending `GET /users/42` produces `{"id": 42}`. A non-integer path value
like `/users/abc` automatically returns a 400 ProblemDetails — the
binder rejects unparseable values before the handler runs.

## Query parameters

Same pattern, different tag:

```go
type ListParams struct {
    Limit    int   `query:"limit"`
    Verified *bool `query:"verified"`
}

r.Get("/users", func(c *router.Context, p ListParams) {
    // p.Limit is 0 if absent; p.Verified is nil if absent.
})
```

Pointer types make optional query parameters explicit: `*bool` is `nil`
when the query string omits the key, set when present. Plain types
default to their zero value.

## Request body

`body:""` on a struct field decodes the entire JSON body into it:

```go
type CreateUserCommand struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type CreateParams struct {
    Body CreateUserCommand `body:""`
}

r.Post("/users", func(c *router.Context, p CreateParams) {
    c.Header("Location", "/users/1")
    c.JSON(http.StatusCreated, p.Body)
})
```

Invalid JSON returns a 400 ProblemDetails automatically.

## Error responses

For domain errors you write the response explicitly. Use the
`router.NotFound`, `router.BadRequest`, etc. constructors to build a
ProblemDetails value:

```go
r.Get("/users/{id}", func(c *router.Context, p GetUserParams) {
    user, ok := store.Get(p.ID)
    if !ok {
        c.JSON(http.StatusNotFound, router.NotFound("user not found"))
        return
    }
    c.JSON(http.StatusOK, user)
})
```

## Route groups

`Group(prefix, opts...)` creates a sub-router with a shared path prefix.
Any options passed to the group apply to every endpoint registered
through it:

```go
import "github.com/joakimcarlsson/minmux/openapi"

api := r.Group("/api/v1", openapi.Tags("v1"))
users := api.Group("/users", openapi.Tags("Users"))

users.Get("/{id}", getUser)
users.Post("", createUser)
```

The `/api/v1/users/{id}` endpoint inherits tags `["v1", "Users"]`.

## OpenAPI generation

Annotate routes with options from the `openapi` package. The spec is
served by calling `Generator.Handler(r)`:

```go
import (
    "github.com/joakimcarlsson/minmux/openapi"
    "github.com/joakimcarlsson/minmux/router"
)

r.Get("/users/{id}", getUser,
    openapi.Summary("Get a user"),
    openapi.Description("Returns a single user by their ID."),
    openapi.Tags("Users"),
    openapi.ReturnsBody[User](http.StatusOK, "User found"),
    openapi.ReturnsBody[router.ProblemDetails](http.StatusNotFound, "User not found"),
)

gen := openapi.NewGenerator(openapi.Info{
    Title:   "Users API",
    Version: "0.1.0",
})
r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))
```

Two annotation functions describe responses:

- `openapi.Returns(status, description)` — response with no body
  (204 No Content, 304 Not Modified, redirects, etc.).
- `openapi.ReturnsBody[T](status, description)` — response with a typed
  JSON body. The generator hoists named struct types into
  `components/schemas` and emits a `$ref` at each use site.

The signature of the handler has no effect on the generated spec — what
you declare with `Returns` / `ReturnsBody` is what appears.

## Full example

`examples/todo/` is a runnable Todo API exercising every feature:
path / query / body parameters, group tag cascading, 200 / 201 / 204
success responses, 400 / 404 error responses, an in-memory store, and
an OpenAPI spec served at `/openapi.json`.

```bash
cd examples/todo
go run .
curl -s http://localhost:8080/openapi.json | jq
```
