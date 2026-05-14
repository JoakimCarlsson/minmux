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

## Forms and file uploads

Two additional tags cover form-encoded payloads and uploads. They share
the same flat-struct binding model as the rest of the framework, and the
OpenAPI generator emits the matching request body shape for each.

`form:"<name>"` binds a single form field (text part). Use it on scalar
Go types or scalar slices:

```go
type LoginParams struct {
    Username string `form:"username"`
    Password string `form:"password" format:"password"`
    Remember *bool  `form:"remember"`
}

r.Post("/login", func(c *router.Context, p LoginParams) {
    // p.Username, p.Password set from the form; p.Remember is nil if omitted.
})
```

A Params struct containing only `form:` fields is bound from an
`application/x-www-form-urlencoded` request and generates a requestBody
with the same content type.

`file:"<name>"` binds a multipart file part. Use `*router.FormFile` for a
single upload or `[]*router.FormFile` to accept multiple files under the
same field name:

```go
type ProfileParams struct {
    DisplayName string             `form:"display_name"`
    Avatar      *router.FormFile   `file:"avatar"  contentType:"image/png, image/jpeg"`
    Photos      []*router.FormFile `file:"photos"`
}

r.Post("/profile", func(c *router.Context, p ProfileParams) {
    f, _ := p.Avatar.Open()
    defer f.Close()
    // p.Avatar.Filename, p.Avatar.Size, p.Avatar.Header.Get("Content-Type")
})
```

`router.FormFile` is an alias for `multipart.FileHeader`, so you call
the stdlib `Open` method to read the contents. The optional
`contentType:"..."` tag is enforced at request time (mismatches return a
400 ProblemDetails) and becomes an OAS 3.2 Encoding Object on the
generated multipart schema.

A Params struct containing any `file:` field is bound from a
`multipart/form-data` request and any `form:` fields in the same struct
become text parts on the same multipart body.

The `body:""` tag also accepts raw streams. When the field type is
`io.Reader` the request body is handed to the handler unbuffered; when
it is `[]byte` the body is buffered up to the router's max-memory cap:

```go
type RawUploadParams struct {
    Body io.Reader `body:"" contentType:"image/png, image/jpeg"`
}

r.Post("/raw", func(c *router.Context, p RawUploadParams) {
    n, _ := io.Copy(os.Stdout, p.Body)
    c.JSON(http.StatusOK, map[string]any{"bytes": n})
})
```

The generated requestBody emits one content-type entry per value in the
`contentType` tag, defaulting to `application/octet-stream` when the tag
is absent.

`form`/`file` fields are required by default; pointer and slice fields
are optional. Missing required fields return a 400 ProblemDetails.

Mixing `body:""` with `form:` or `file:` in the same Params struct is a
registration error — pick one request shape per endpoint.

The in-memory multipart cap and `[]byte` body cap are both controlled by
a single router option:

```go
r := router.New(router.WithMaxMultipartMemory(16 << 20)) // 16 MiB
```

The default is 32 MiB. Multipart parts larger than the cap are spilled
to a temp file on disk by `mime/multipart`; the cap on `[]byte` body
fields is strict — oversize requests get a 400 ProblemDetails.

## Field formats

Numeric Go types map automatically to OAS 3.2 formats: `int32`/`uint32`
become `format: int32`, `int64`/`int`/`uint64`/`uint` become
`format: int64`, `float32` becomes `format: float`, and `float64`
becomes `format: double`. Unsigned types also get `minimum: 0`.

For string formats that the type system can't tell us about (`email`,
`password`, `uuid`, `uri`, `date`, etc.), add a `format:"..."` struct
tag. It works on parameter fields and body/response struct fields, and
the value is passed through to the generated schema as-is:

```go
type SignupCommand struct {
    Email    string `json:"email"    format:"email"`
    Password string `json:"password" format:"password"`
}

type LookupParams struct {
    UserID string `query:"user_id" format:"uuid"`
}
```

The tag also overrides the auto-inferred numeric format on the rare
occasion you want to widen an `int32` field to `int64` in the spec,
for example.

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
