# Client Generation

`clientgen` turns the OpenAPI document [`openapi`](modules.md) derives from
your handler signatures into a typed Go client.

The point is to close the loop. The server spec is already drift-free —
it is read straight off the registered handlers. Without a generated
client, anyone calling that API from another Go program hand-writes the
HTTP layer, and that hand-written layer drifts the moment an endpoint
changes. With one, the same handler signatures produce both halves, so a
breaking change to a request or response type is a **compile error in the
caller** instead of a runtime 4xx.

```bash
go get github.com/joakimcarlsson/minmux/clientgen
```

## Generating

Put the router construction somewhere both the server and the generator
can import, then call `Generate` from a small `go:generate` program.

```go
// gen/main.go
package main

import (
    "log"

    "github.com/joakimcarlsson/minmux/clientgen"
    "example.com/myapi/todoapi"
)

func main() {
    r, _ := todoapi.NewRouter()

    err := clientgen.Generate(clientgen.Config{
        Spec:    todoapi.Spec(r),
        Package: "todoclient",
        Out:     "client",
        Command: "go generate ./...",
    })
    if err != nil {
        log.Fatalf("generating client: %v", err)
    }
}
```

```go
//go:generate go run ./gen
```

`go generate ./...` then writes three files into `client/`:

| File | Contents |
|---|---|
| `client.gen.go` | The `Client` type, options, `APIError`, and transport plumbing |
| `types.gen.go` | One Go type per component schema |
| `operations.gen.go` | One params struct and one method per operation |

If all you have is a spec file, the `minmux-clientgen` binary is a thin
wrapper over the same entry point:

```bash
go run github.com/joakimcarlsson/minmux/clientgen/cmd/minmux-clientgen \
    -spec openapi.json -package todoclient -out ./client
```

Prefer the library path when the server is Go you can import — it hands
the generator the in-memory document and skips the JSON round trip.

## Calling

Every operation is one method taking a context and a single params struct:

```go
c, err := todoclient.New("https://api.example.com")
if err != nil {
    return err
}

todo, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
    Body: todoclient.CreateTodoCommand{Title: "write the client"},
})
```

A single params struct per operation — rather than positional arguments —
mirrors the `func(c *router.Context, p Params)` shape on the server, and
means adding an optional query parameter stays source-compatible for
existing callers.

Path parameters are escaped, optional query and header parameters are
omitted when `nil`, and `{name...}` wildcards keep their separators.

### Optional versus required

The generator reads the schema's `required` array to decide between a
value and a pointer. A property the server always sends is a value type; an
optional one is a pointer, so the caller can tell *absent* from *zero*:

```go
type UpdateTodoCommand struct {
    Title     *string `json:"title,omitempty"`
    Completed *bool   `json:"completed,omitempty"`
}
```

That distinction is what makes partial updates work. Setting only
`Completed` leaves the stored title alone; if these collapsed to value
types, the same call would blank it.

On the server side a field is required unless it is a pointer or carries
`omitempty` / `omitzero` — the same rule `encoding/json` applies.

### Errors

Any response the operation does not declare as its success status becomes
an `*APIError`, with the body decoded into the type the spec names for
that status:

```go
todo, err := c.GetTodo(ctx, todoclient.GetTodoParams{ID: 42})
if err != nil {
    if pd, ok := todoclient.ErrorValue[*todoclient.ProblemDetails](err); ok {
        log.Printf("%d: %s", pd.Status, *pd.Detail)
    }

    var apiErr *todoclient.APIError
    if errors.As(err, &apiErr) {
        log.Println(apiErr.StatusCode, string(apiErr.Body))
    }
    return err
}
```

`APIError.Body` always carries the raw bytes. `Value` is populated only
when the body actually decodes into the declared type — a mismatched
payload degrades to raw bytes rather than surfacing a misleading zero
value.

### Streaming

Streaming operations return an `iter.Seq2` over decoded items:

```go
events, err := c.StreamTodoEvents(ctx, todoclient.StreamTodoEventsParams{})
if err != nil {
    return err
}
for ev, err := range events {
    if err != nil {
        return err
    }
    log.Println(ev.Kind, ev.Todo.Title)
}
```

| Response media type | Return |
|---|---|
| `application/jsonl`, `x-ndjson`, `json-seq`, `geo+json-seq` | `iter.Seq2[T, error]` |
| `text/event-stream` | `iter.Seq2[SSEEvent, error]` |
| `multipart/mixed` | `iter.Seq2[Part, error]` |

The sequence owns the response body and closes it when iteration ends,
including on an early `break`. A caller that never iterates leaks the
connection.

SSE events keep their raw payload so nothing is lost on non-JSON streams;
decode a JSON payload with `SSEData`:

```go
progress, err := todoclient.SSEData[todoclient.Progress](ev)
```

## Transport

The generated client owns no policy. Auth, retries, tracing, and
cross-host routing layer on through a pluggable `*http.Client` or
`http.RoundTripper`:

```go
c, err := todoclient.New("https://api.example.com",
    todoclient.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
    todoclient.WithTransport(otelhttp.NewTransport(nil)),
    todoclient.WithHeader("X-Tenant", "acme"),
    todoclient.WithRequestHook(func(r *http.Request) error {
        r.Header.Set("Authorization", "Bearer "+token())
        return nil
    }),
)
```

`WithTransport` copies the underlying client rather than mutating it, so
sharing one `*http.Client` between API clients is safe.

## Guarding against drift

Output is byte-stable for a given document — types, operations, fields,
and imports are all emitted in sorted order and run through `go/format`.
Regenerating an unchanged API produces no diff, which makes the committed
client checkable in CI:

```bash
make verify-generate
```

or directly:

```bash
go generate ./... && git diff --exit-code
```

A Go test works just as well, and fails closer to the code:

```go
func TestGeneratedClientIsUpToDate(t *testing.T) {
    r, _ := todoapi.NewRouter()
    files, err := clientgen.Render(clientgen.Config{
        Spec:    todoapi.Spec(r),
        Package: "todoclient",
        Command: "go generate ./...",
    })
    // ... compare each file against client/<name>
}
```

`Render` returns the files in memory without touching disk, which is also
the hook for writing output somewhere other than a directory.

## Discriminated unions

A schema registered with `openapi.Generator.RegisterUnion` becomes a
tagged wrapper with one typed accessor per variant:

```go
for _, block := range page.Blocks {
    if hero, ok := block.AsHeroBlock(); ok {
        render(hero.Headline)
        continue
    }
    if text, ok := block.AsTextBlock(); ok {
        render(text.Body)
    }
}
```

An unrecognized discriminator leaves `Value` nil but keeps the original
bytes in `Raw`, so a client compiled against an older spec round-trips a
variant the server added later instead of dropping it.

## Naming

Method names come from `operationId`, so they are stable against
incidental route changes. minmux derives one automatically
(`GET /todos/{id}` → `getTodosById`), but pinning it is worthwhile for a
client you publish:

```go
todos.Get("/{id}", a.Get,
    openapi.OperationID("getTodo"),
    openapi.ReturnsBody[Todo](http.StatusOK, "The todo"),
)
```

Wire names become idiomatic Go identifiers, with common initialisms
upper-cased: `created_at` → `CreatedAt`, `X-Request-Id` → `XRequestID`.

## Limitations

- **Integer widths follow the spec, not the server.** minmux emits
  `format: int64` for a Go `int`, so the generated field is `int64`. The
  two are wire-compatible; only the Go type differs.
- **Multiple success statuses with different body types.** A single typed
  return cannot express both. The lowest 2xx wins, and the generated
  method documents which statuses yield the zero value.
- **Enums are not generated as constants.** An `enum:"..."` tag
  constrains the served spec but produces a plain `string` field.

## Example

`examples/clientgen` in the repository is the whole loop end to end: a
server, the `go:generate` step, the committed client, and a round-trip
test that runs the generated methods against `httptest`.
