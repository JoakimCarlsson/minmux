# Streaming

minmux has first-class support for the OpenAPI 3.2 streaming media types:

| Media type                  | Use case                              |
|-----------------------------|---------------------------------------|
| `text/event-stream`         | Server-Sent Events, AI token streams  |
| `application/jsonl`         | JSON Lines (newline-framed records)   |
| `application/x-ndjson`      | Same framing, common alias            |
| `application/json-seq`      | RFC 7464 record-separator framing     |
| `application/geo+json-seq`  | GeoJSON Sequences                     |
| `multipart/mixed`           | Heterogeneous part streams            |

Each maps to a runtime helper that handles the wire format and to an
OpenAPI option that emits the matching `itemSchema` / `itemEncoding` /
`prefixEncoding` keywords from OAS 3.2 §4.14.3.

The package idiom splits **output** (handlers push frames, mirroring
`c.JSON(...)`) from **input** (handlers pull frames via Go 1.23
`iter.Seq2[T, error]`, mirroring the `body:""` struct tag).

A runnable end-to-end showcase lives in `examples/streams/`.

## Server-Sent Events

```go
import "github.com/joakimcarlsson/minmux/router"

type Token struct {
    Index int    `json:"index"`
    Text  string `json:"text"`
}

func tokens(c *router.Context) {
    sse := c.SSE(http.StatusOK)
    defer sse.Close()
    for i, word := range words {
        if err := sse.Send(router.SSEEvent{
            ID:    fmt.Sprintf("%d", i),
            Event: "token",
            Data:  Token{Index: i, Text: word}, // encoded as JSON
        }); err != nil {
            return // client gone
        }
    }
}

r.Get("/tokens", tokens,
    openapi.Summary("Streaming AI tokens via SSE"),
    openapi.SSEStream[Token](http.StatusOK, "Token stream"),
)
```

`c.SSE` sets `Content-Type: text/event-stream`,
`Cache-Control: no-cache`, `Connection: keep-alive`, and
`X-Accel-Buffering: no` on the first `Send`, flushes the underlying
`http.Flusher` after every frame, and short-circuits as soon as the
request context is cancelled.

`SSEEvent.Data` is rendered per the WHATWG SSE specification:

- `string` / `[]byte` are written verbatim and split into one `data:` line
  per `\n`.
- Any other value is JSON-encoded onto a single `data:` line via the
  router codec.
- `Comment`, `ID`, `Event`, and `Retry` are emitted as `:`, `id:`,
  `event:`, and `retry:` lines respectively.

`openapi.SSEStream[T]` emits the canonical event schema from
OAS 3.2 §4.14.4 (`{data, event, id, retry}` with `data` required), and
when `T` is non-empty it also annotates `data` with
`contentMediaType: application/json` + `contentSchema: T` so clients
know to parse the data field as a JSON-encoded `T`.

Receiving SSE in a handler uses an `iter.Seq2[router.SSEEvent, error]`
body field. The parser handles multi-line `data:`, ignores comments
and unknown fields, and combines `data:` lines per the spec:

```go
type EventsParams struct {
    Events iter.Seq2[router.SSEEvent, error] `body:"" contentType:"text/event-stream"`
}

func ingest(c *router.Context, p EventsParams) {
    for ev, err := range p.Events {
        if err != nil { /* malformed frame */ }
        // ev.ID / ev.Event / ev.Data (string) / ev.Retry
    }
}
```

## JSON Lines / NDJSON / JSON-seq

Use `c.Stream(status, contentType)` for any of the line- or
record-separator-framed sequential JSON types. The framer is picked
from the Content-Type — newline for `application/jsonl` and
`application/x-ndjson`, `0x1E`-prefixed lines for `application/json-seq`
and `application/geo+json-seq`.

```go
type LogEntry struct {
    Timestamp time.Time `json:"timestamp"`
    Level     string    `json:"level"`
    Message   string    `json:"message"`
}

func tail(c *router.Context) {
    w := c.Stream(http.StatusOK, "application/jsonl")
    defer w.Close()
    for entry := range source {
        if err := w.Send(entry); err != nil { return }
    }
}

r.Get("/logs.jsonl", tail,
    openapi.StreamsBody[LogEntry](
        http.StatusOK, "Newline-delimited log entries",
        "application/jsonl", "application/x-ndjson",
    ),
)
```

`openapi.StreamsBody[T]` emits one MediaType per content type, each
carrying `itemSchema: T`. If no content types are listed it defaults to
`application/jsonl`.

A streaming request body uses `iter.Seq2[T, error]`. One endpoint can
accept any combination of the sequential JSON types — the framer is
dispatched per request from the `Content-Type` header:

```go
type IngestParams struct {
    Logs iter.Seq2[LogEntry, error] `body:"" contentType:"application/jsonl, application/x-ndjson, application/json-seq"`
}

func ingest(c *router.Context, p IngestParams) {
    var ok, bad int
    for entry, err := range p.Logs {
        if err != nil { bad++; continue }
        store(entry); ok++
    }
    c.JSON(http.StatusOK, map[string]int{"accepted": ok, "rejected": bad})
}
```

Decode errors are surfaced through the iterator (the loop sees a zero
value plus a non-nil error) so a single malformed record never crashes
the whole stream.

## multipart/mixed

Use `c.MultipartMixed(status)` for heterogeneous part streams. The
boundary is generated automatically and embedded in the Content-Type
header on the first `Part` call.

```go
func frames(c *router.Context) {
    mp := c.MultipartMixed(http.StatusOK)
    defer mp.Close()
    for _, f := range source {
        mp.Part(
            textproto.MIMEHeader{
                "Content-Type":  {"application/octet-stream"},
                "X-Frame-Index": {strconv.Itoa(f.Index)},
            },
            bytes.NewReader(f.Bytes),
        )
    }
}

r.Get("/frames", frames,
    openapi.MultipartMixedStream[FrameMeta](
        http.StatusOK, "Streaming frames",
        openapi.WithItemContentType("application/octet-stream"),
    ),
)
```

`MultipartMixedStream[T]` emits the OAS 3.2 §4.15.4.8 shape:
`itemSchema` for the repeating part body and `itemEncoding` for the
part's Content-Type. Use `WithPrefixParts(parts ...*openapi.Encoding)`
to declare a fixed sequence of leading parts before the repeating tail
(see §4.15.4.7); the runtime handler must then emit those prefix parts
first. Use `c.Multipart(status, mediaType)` for other multipart
subtypes (`multipart/byteranges`, …).

Receiving multipart/mixed in a handler uses
`iter.Seq2[router.Part, error]`. The boundary is parsed from the
request's Content-Type:

```go
type MixedParams struct {
    Parts iter.Seq2[router.Part, error] `body:"" contentType:"multipart/mixed"`
}

func ingestParts(c *router.Context, p MixedParams) {
    for part, err := range p.Parts {
        if err != nil { /* malformed boundary */ }
        // part.Header is textproto.MIMEHeader
        // part.Body is io.Reader, valid until the next iteration
    }
}
```

## Backpressure and cancellation

Every stream writer:

- Writes the response headers and status code on the first `Send` /
  `Part`. The first call also caches a reference to the underlying
  `http.Flusher` (or `nil` if the response writer does not implement it).
- Calls `Flush()` after each frame so the bytes actually reach the
  client immediately.
- Cheaply checks `c.Request.Context()` before each write and returns the
  context error if the client has disconnected.
- Returns `router.ErrStreamClosed` if `Send` is called after `Close`.

Always defer `Close` — for multipart it writes the terminating boundary,
and for all writers it flushes any pending bytes.

## Spec output

Hitting `/openapi.json` on the streams showcase produces, among other
things:

```json
{
  "/tokens": {
    "get": {
      "responses": {
        "200": {
          "description": "Token stream",
          "content": {
            "text/event-stream": {
              "itemSchema": {
                "type": "object",
                "required": ["data"],
                "properties": {
                  "data": {
                    "type": "string",
                    "contentMediaType": "application/json",
                    "contentSchema": { "$ref": "#/components/schemas/Token" }
                  },
                  "event": { "type": "string" },
                  "id":    { "type": "string" },
                  "retry": { "type": "integer", "minimum": 0 }
                }
              }
            }
          }
        }
      }
    }
  }
}
```
