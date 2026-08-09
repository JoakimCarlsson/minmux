# Compression

`github.com/joakimcarlsson/minmux/compress` is a response compression
middleware that operates on the standard `http.Handler` interface. It's
independent of the router — you can use it with any Go HTTP server — but
plugs into minmux via `Router.Use`.

## Install

```bash
go get github.com/joakimcarlsson/minmux/compress
```

Zero external dependencies: gzip and deflate come from the standard
library.

## Default

`compress.Default()` negotiates gzip or deflate against `Accept-Encoding`
and compresses textual and JSON-like responses of at least 1 KiB.

```go
import (
    "github.com/joakimcarlsson/minmux/compress"
    "github.com/joakimcarlsson/minmux/router"
)

r := router.New()
r.Use(compress.Default())
```

## Configuration

`compress.New(Options)` returns a middleware tuned to your workload.

```go
r.Use(compress.New(compress.Options{
    Level:   flate.BestSpeed,
    MinSize: 4096,
    Types:   []string{"application/json", "text/"},
    Skip: func(r *http.Request) bool {
        return strings.HasPrefix(r.URL.Path, "/api/tiles/")
    },
}))
```

## Options

| Field | Purpose |
|---|---|
| `Level` | Compression level, from `flate.BestSpeed` to `flate.BestCompression`. Zero selects `flate.DefaultCompression`. A level outside the range flate accepts is replaced by the default rather than rejected. |
| `MinSize` | Body bytes a response must reach before it is compressed. Zero selects `DefaultMinSize` (1024). Negative compresses every eligible response regardless of size. |
| `Types` | Content types to compress, matched as **prefixes** of the response media type — `"text/"` covers every textual subtype. Empty selects `DefaultTypes`. |
| `Skip` | `func(*http.Request) bool`. Returns true to leave the response alone. For paths that stream, that serve pre-compressed payloads, or that a proxy already handles. |

`DefaultTypes` is `application/json`, `application/javascript`,
`application/manifest+json`, `application/wasm`, `application/xhtml+xml`,
`application/xml`, `image/svg+xml`, and `text/`. Already-compressed
formats — images, video, archives — are deliberately absent: running
them through a second compressor spends CPU to add bytes.

## What is left alone

The middleware only acts where it can improve the response. It passes
through untouched when:

- the client's `Accept-Encoding` asks for neither gzip nor deflate;
- `Skip` returns true;
- the handler set its own `Content-Encoding` — a body the handler
  encoded belongs to the handler;
- the media type isn't in `Types`;
- the response is partial (`206`, or any `Content-Range`) — re-encoding
  would invalidate the ranges it answers;
- the status is defined to carry no body (`204`, `304`);
- the body never reaches `MinSize`.

## Negotiation

`Accept-Encoding` is parsed with quality values, including explicit
refusals:

| Header | Chosen |
|---|---|
| `gzip, deflate` | gzip |
| `deflate, gzip` | gzip (ties go to server preference) |
| `gzip;q=0.5, deflate;q=0.9` | deflate |
| `gzip;q=0, deflate` | deflate |
| `*` | gzip |
| `br` | none |
| `identity` | none |

`Negotiate` is exported, so the same choice is available outside the
middleware — for a handler that serves pre-compressed blobs and wants to
answer in whatever encoding it has on disk.

## Headers

A response that could have been compressed gains `Vary: Accept-Encoding`,
whether or not it ended up compressed, so a shared cache never serves a
gzipped body to a client that can't read one.

When a response is compressed the middleware sets `Content-Encoding` and
removes `Content-Length` — the length the handler announced was the
uncompressed one, and the encoded length isn't known until the stream
closes. `Accept-Ranges` is removed for the same reason: the byte offsets
it advertises no longer describe what's on the wire.

## Buffering, streaming, and hijacking

The decision to compress needs two things the handler reveals at
different moments: the content type, known when headers are written, and
the body size, known only as the body arrives. So the middleware holds
the response back until the body reaches `MinSize` — that threshold is
also the per-request buffer ceiling — and only then commits to an
encoding and sends the headers.

`Flush` settles the response as it stands. A handler that streams is
never held in the buffer waiting for a threshold it may never reach; a
stream that hasn't yet earned compression is sent uncompressed rather
than committed to an encoding on the strength of its first few bytes.
Server-sent events therefore work unchanged, and `text/event-stream`
isn't in `DefaultTypes` anyway.

`Hijack` is forwarded to the underlying writer, so websocket upgrades
work through the middleware. `Unwrap` exposes the wrapped writer to
`http.ResponseController`.

Compressors are pooled per encoding and level, so a server running large
bodies through the middleware reuses their window and hash tables
instead of allocating a fresh compressor per request.

## Ordering against the output cache

Where you register the middleware relative to
[`outputcache`](../caching/outputcache.md) decides what the cache stores.

**Inside the cache** — added with `Use` *after* the cache's middleware,
so it runs closer to the handler:

```go
r.Use(cache.Middleware())
r.Use(compress.Default())
```

A cached route is then compressed once per entry rather than once per
request, and the cache stores the encoded bytes. That's the cheaper
arrangement for large, hot responses, and it makes the encoding part of
what the entry *is* — so those routes must also vary by it:

```go
r.Get("/api/report", handler,
    outputcache.WithOutputCache(5*time.Minute,
        outputcache.VaryByEncoding(),
    ),
)
```

Without `VaryByEncoding`, a client that asked for no compression can be
handed an entry it cannot read.

Note what that keys on: `VaryByEncoding` is `VaryByHeader("Accept-Encoding")`,
so the key is the **header string**, not the encoding negotiated from it.
`gzip, deflate`, `deflate, gzip` and `gzip;q=0, deflate` are three entries
even where two of them produce the same body. Real browsers send a small
number of distinct values, so this is usually a handful of entries per
route — but it is why the outside-the-cache ordering is the better trade
when clients' `Accept-Encoding` varies widely.

**Outside the cache** — registered before it — entries stay
uncompressed and every hit is re-encoded on the way out. That costs CPU
per request but keeps one entry per route, which is the better trade
when bodies are small or when clients' `Accept-Encoding` varies widely.
