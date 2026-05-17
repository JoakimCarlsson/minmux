# Feature Tour

A guided walk through minmux's surface — routing, typed parameter
binding, middleware, request bodies, forms and uploads, schema
constraints, response headers, OpenAPI annotations, and the served
spec. Each section is self-contained; skim or read top-to-bottom.

For a five-minute "build and run your first endpoint" intro, see
[Quick Start](getting-started/quick-start.md). The complete source for
the patterns shown below lives in `examples/todo/`.

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

## HTTP methods

`Router` and `Group` expose a typed registration method for each of the
common HTTP verbs:

```go
r.Get("/widgets",         listWidgets)
r.Post("/widgets",        createWidget)
r.Put("/widgets/{id}",    replaceWidget)
r.Patch("/widgets/{id}",  patchWidget)
r.Delete("/widgets/{id}", deleteWidget)
r.Options("/widgets",     describeMethods)
r.Head("/widgets/{id}",   probeWidget)
```

All seven verbs flow through the same dispatcher, so they all support
typed Params binding, per-route middleware, and the full
`openapi.*` option set, and they all appear in the generated spec under
their corresponding OAS 3.2 operation slot.

`net/http` already auto-serves `HEAD` for any registered `GET` handler
by suppressing the response body, so reach for `r.Head` only when you
need behavior that diverges from the matching `GET` — for example, a
faster existence probe that skips an expensive load, or different
response headers. `OPTIONS` is more commonly hand-written: CORS
preflights are usually handled by middleware, but the typed registration
is the right tool when you want to advertise a custom `Allow` set, run
auth probes, or expose capability discovery (e.g. WebDAV-style).

For raw method/path registration that bypasses the typed pipeline and
the OpenAPI generator — useful for serving static handlers like the
spec itself — use `r.HandleFunc(method, path, handler)` or
`r.Handle(method, path, handler)`.

The full runnable example for these verbs lives in `examples/verbs/`.

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

A handler that doesn't bind a path variable is still valid — the
OpenAPI generator auto-fills any unbound `{name}` template segment with
a required `string` path parameter so the spec stays conformant to
OAS 3.2 §4.4.1.1. Bind a Params field with `path:"name"` when you want
a richer schema or a typed value in your handler.

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

The same pointer-vs-value distinction drives the generated OpenAPI:
a non-pointer scalar field emits `required: true`, while pointer (and
slice) fields are optional. This matches form fields and lets the spec
faithfully describe what the handler actually expects. Header
parameters (`header:"X-Trace-Id"`) follow the same rule.

To mark a parameter deprecated — useful for legacy query keys you're
keeping around for back-compat — add `deprecated:"true"` to the tag:

```go
type ListParams struct {
    Cursor *string `query:"cursor" deprecated:"true"` // optional + deprecated
    Limit  int    `query:"limit"`
}
```

Operations themselves can be deprecated via the `openapi.Deprecated()`
option:

```go
r.Get("/users/old", listLegacy,
    openapi.Summary("List users (legacy)"),
    openapi.Deprecated(),
)
```

Renderers like Scalar and Swagger UI style deprecated parameters and
operations distinctly (strikethrough, banner) so consumers see them at a
glance.

## Panic recovery

A panic in a handler bubbles up through `net/http` and returns a bare
500 with a Go-style stack trace. Wire `router.Recover()` at the top of
the middleware chain so panics become 500 ProblemDetails responses
with the stack logged separately:

```go
r := router.New()
r.Use(router.Recover())
```

`router.Recover` honors `http.ErrAbortHandler` — it re-panics so
`net/http`'s connection-close behavior still works. It defaults to
`log.Default()` for stack logging; use `router.RecoverWith(logger)` to
pass a custom `*log.Logger`.

Note: if a handler has already written response headers before
panicking, the 500 body can't be cleanly emitted — net/http will have
flushed the earlier status. The panic is still logged. Streaming
handlers that panic mid-stream will simply close the connection.

## Middleware

`Router.Use` attaches a middleware to *every* request — useful for
logging, CORS, recovery, etc. For middleware that should run only on
specific routes, use the `router.Middleware(...)` option, which works
at both endpoint and group scope:

```go
// One route only.
r.Get("/admin/stats", stats,
    router.Middleware(requireAdmin),
)

// Whole group — cascades to every route registered through it.
api := r.Group("/api", router.Middleware(requestID))

api.Get("/users/{id}", getUser,
    // Route-level middleware composes on top of the group's.
    // Order at runtime: requestID → rateLimit → handler.
    router.Middleware(rateLimit),
)
```

The middleware signature is the standard `func(http.Handler) http.Handler`,
so the entire stdlib middleware ecosystem (and the `cors`,
`outputcache` modules) plug in directly. The first middleware in the
slice is the outermost wrapper — same order as `Router.Use`.

## External docs links

OpenAPI lets you attach a "see also" link at three scopes; minmux
supports all three:

```go
// Per operation
r.Post("/auth/device", deviceLogin,
    openapi.ExternalDocsLink(
        "https://datatracker.ietf.org/doc/html/rfc8628",
        "RFC 8628 — OAuth 2.0 Device Authorization Grant",
    ),
)

// Per tag (see Structured tags below)
gen.Tags = []*openapi.Tag{
    {Name: "Streams", ExternalDocs: &openapi.ExternalDocs{
        URL: "https://example.com/docs/streaming",
    }},
}

// Document-wide
gen.ExternalDocs = &openapi.ExternalDocs{
    URL: "https://github.com/example/api",
}
```

Pass an empty URL to `ExternalDocsLink` to clear a previously-set link
— useful when composing options through helpers.

## Document metadata

The `Info` block carries the API's public identity. Beyond `Title` /
`Version` / `Description`, minmux supports the full OAS 3.2 Info
Object:

```go
gen := openapi.NewGenerator(openapi.Info{
    Title:          "Pets API",
    Version:        "0.1.0",
    Summary:        "Pet store catalog and management",
    Description:    "Long-form Markdown describing the API…",
    TermsOfService: "https://example.com/tos",
    Contact: &openapi.Contact{
        Name:  "API Team",
        URL:   "https://example.com/contact",
        Email: "api@example.com",
    },
    License: &openapi.License{
        Name:       "Apache-2.0",
        Identifier: "Apache-2.0", // SPDX expression (3.1+)
        // or URL: "https://www.apache.org/licenses/LICENSE-2.0"
    },
})
```

`License.Identifier` (SPDX) and `License.URL` are mutually exclusive per
spec — set one. Scalar surfaces all of these in the API reference
header.

## Structured tags

`openapi.Tags("Pets")` on a route attaches a string label to the
operation. To give that label real metadata — a description, an
external-docs link, or a parent for nested navigation — declare a Tag
Object on the generator:

```go
gen := openapi.NewGenerator(openapi.Info{Title: "API", Version: "0.1.0"})

gen.Tags = []*openapi.Tag{
    {
        Name:        "Catalog",
        Summary:     "Resources",
        Description: "Domain resources exposed by the API.",
        Kind:        "nav",
    },
    {
        Name:        "Pets",
        Parent:      "Catalog",
        Description: "Pet CRUD.",
    },
    {
        Name:        "Users",
        Parent:      "Catalog",
        Description: "Authenticated user profile.",
    },
    {
        Name:        "Streams",
        Description: "SSE / JSONL / multipart streams.",
        ExternalDocs: &openapi.ExternalDocs{
            URL:         "https://example.com/docs/streaming",
            Description: "Streaming reference",
        },
    },
}

gen.ExternalDocs = &openapi.ExternalDocs{
    URL: "https://github.com/example/api",
}
```

`Parent` is one of OAS 3.2's headline features — it builds nested tag
groups so renderers like Scalar can show a real navigation tree
("Catalog → Pets") instead of a flat list. `Kind` is a free-form
classifier (`"nav"` for navigation grouping is the common convention).
`ExternalDocs` attaches a "see also" link to the tag.

Tag names referenced by operations (`openapi.Tags("Pets")`) match by
string against the `Name` field of these Tag Objects — operations don't
need to change when you switch from bare strings to structured tags.

## Schema constraints

Struct-field tags map 1:1 to the OAS schema keywords most clients care
about. They apply to path, query, header, and body fields alike:

| Tag | Maps to | Applies to |
|---|---|---|
| `minimum:"N"` | `minimum` | numeric fields |
| `maximum:"N"` | `maximum` | numeric fields |
| `minLength:"N"` | `minLength` | string fields |
| `maxLength:"N"` | `maxLength` | string fields |
| `pattern:"<regex>"` | `pattern` | string fields |
| `enum:"a,b,c"` | `enum` | scalar fields (type-aware) |
| `default:"x"` | `default` | scalar fields (type-aware) |

```go
type ListPetsParams struct {
    Tag    *string `query:"tag"    enum:"dog,cat,bird,fish"`
    Limit  int     `query:"limit"  minimum:"1" maximum:"100" default:"20"`
}

type CreatePetCommand struct {
    Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[A-Za-z ]+$"`
    Tag  string `json:"tag"  enum:"dog,cat,bird,fish"`
}
```

`enum` and `default` are coerced to the field's Go type, so an integer
field with `enum:"1,2,3"` marshals as JSON numbers (`"enum":[1,2,3]`)
rather than strings. Unparseable values are silently dropped — the spec
stays valid; the constraint just doesn't take effect.

These tags are documentation-only today: minmux generates the schema
but does not validate incoming requests against it. Renderers like
Scalar surface the constraints in "Try it" forms and code samples; the
handler is still responsible for runtime validation.

## Response headers

CRUD APIs typically return useful headers — `Location` on `201 Created`,
`ETag` and `Cache-Control` on cacheable reads, `Retry-After` when
rate-limited. Declare them on a response via `openapi.WithHeader`:

```go
r.Post("/pets", createPet,
    openapi.ReturnsBody[Pet](http.StatusCreated, "Pet created",
        openapi.WithHeader("Location", "URL of the new pet"),
    ),
)

r.Get("/pets/{id}", getPet,
    openapi.ReturnsBody[Pet](http.StatusOK, "Pet",
        openapi.WithHeader("ETag", "Opaque revision marker"),
        openapi.WithHeader("Cache-Control", "Cache hints (e.g. max-age=60)"),
    ),
)
```

The default schema is a plain string. Override for typed headers like
`Retry-After` (integer seconds) using `openapi.WithHeaderSchema`:

```go
openapi.WithHeader("Retry-After", "Seconds before retrying",
    openapi.WithHeaderSchema(&openapi.Schema{Type: "integer", Format: "int32"}),
)
```

`WithHeader` is a `ResponseOption` — it attaches to the surrounding
`Returns` / `ReturnsBody` declaration, not the operation as a whole, so
each status code gets its own headers map. `Returns` accepts headers too,
which matters for bodyless responses like `204 No Content` carrying an
`X-Trace-Id`.

## operationId

Every operation in the generated spec carries an `operationId` — the
stable identifier client generators use to name the corresponding method
(`getPetById`, `listPets`, etc.). minmux derives one automatically from
the HTTP method and route path:

| Route | Derived `operationId` |
|---|---|
| `GET /pets` | `getPets` |
| `POST /pets` | `postPets` |
| `GET /pets/{id}` | `getPetsById` |
| `POST /users/me/password` | `postUsersMePassword` |
| `GET /streams/logs.jsonl` | `getStreamsLogsJsonl` |

Non-alphanumeric characters in path segments (`.`, `-`) become word
breaks so the result is always a valid identifier.

Override the derived name with `openapi.OperationID(...)` when you want
a nicer generated client method name or to lock the public client API
against incidental route renames:

```go
r.Get("/pets", listPets,
    openapi.OperationID("listPets"), // overrides the derived "getPets"
)
```

`operationId` must be unique per document. The default derivation is
collision-free by construction (method+path is unique on a router); if
you override, the burden of uniqueness moves to you.

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

## Static files and SPAs

`Router.Static(prefix, http.FileSystem)` mounts a generic file server.
Hashed or versioned assets are served as-is; missing paths return 404.

```go
r.Static("/assets/", http.Dir("./public/assets"))
```

`Router.SPA(fs.FS)` mounts a single-page app at `/`. Requests for files
that exist in the FS are served as-is; any other GET path serves
`index.html` so the client-side router can claim the URL. Returns an
error if `index.html` is missing.

```go
import (
    "embed"
    "io/fs"
)

//go:embed all:dist
var dist embed.FS

distFS, _ := fs.Sub(dist, "dist")
if err := r.SPA(distFS); err != nil {
    log.Fatal(err)
}
```

More specific routes (`/api/v1/...`, `/openapi.json`, `/docs`) take
precedence over the SPA catch-all per `net/http` ServeMux semantics, so
mount them on the same router without conflict.

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

For streaming responses (SSE, JSONL, NDJSON, JSON-seq, multipart/mixed),
use the dedicated `openapi.StreamsBody[T]`, `openapi.SSEStream[T]`, and
`openapi.MultipartMixedStream[T]` options paired with the matching
`c.Stream` / `c.SSE` / `c.MultipartMixed` runtime helpers — see the
[Streaming](streaming.md) page.

For authenticated APIs, register security schemes on the generator and
declare per-endpoint requirements with `openapi.Security`,
`openapi.SecurityAll`, `openapi.OptionalSecurity`, and
`openapi.NoSecurity` — see the [Security](security.md) page.

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
