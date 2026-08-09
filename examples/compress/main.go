// Command compress is a runnable showcase of the compression middleware.
//
// It serves one large JSON body, one small error body, one pre-encoded blob
// and one SSE stream, so every branch of the middleware is reachable with
// curl, and it registers the compressor inside the output cache to show what
// that ordering does to a cached route.
//
//	go run .
//
//	# compressed, and the whole point of the package
//	curl -s -o /dev/null -w '%{size_download}\n' \
//	    -H 'Accept-Encoding: gzip' localhost:8080/partition
//
//	# the same route uncompressed, for comparison
//	curl -s -o /dev/null -w '%{size_download}\n' localhost:8080/partition
//
//	# deflate when that is what the client prefers
//	curl -sD- -o /dev/null \
//	    -H 'Accept-Encoding: deflate' localhost:8080/partition
//
//	# an explicit refusal is honoured
//	curl -sD- -o /dev/null \
//	    -H 'Accept-Encoding: gzip;q=0' localhost:8080/partition
//
//	# too small to be worth a gzip frame
//	curl -sD- -o /dev/null \
//	    -H 'Accept-Encoding: gzip' localhost:8080/unavailable
//
//	# the handler encoded this one itself; the middleware leaves it alone
//	curl -sD- -o /dev/null -H 'Accept-Encoding: gzip' localhost:8080/blob
//
//	# a stream is not held back waiting to reach the size threshold
//	curl -N -H 'Accept-Encoding: gzip' localhost:8080/ticks
//
//	# the cached route ran its handler once, whatever the encodings asked for
//	curl -s localhost:8080/stats
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joakimcarlsson/minmux/compress"
	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/outputcache"
	"github.com/joakimcarlsson/minmux/outputcache/inmemory"
	"github.com/joakimcarlsson/minmux/router"
)

// Partition stands in for the kind of body this middleware exists for: a wide
// array of floats, which is where JSON is at its least efficient — about
// fifty bytes of text per number that a binary format would carry in four.
type Partition struct {
	Points []float32 `json:"points"`
}

// Stats reports how many times the cached handler actually ran, which is the
// observable difference between compressing inside the cache and outside it.
type Stats struct {
	HandlerRuns int64 `json:"handlerRuns"`
}

// Failure is a small error body, below the size threshold.
type Failure struct {
	Error string `json:"error"`
}

// partitionCacheTTL is long enough that the demo's repeated curls all hit the
// same entry.
const partitionCacheTTL = time.Minute

var handlerRuns atomic.Int64

// points builds a body large enough to be worth compressing and repetitive
// enough to compress well, which is exactly the shape of the responses this
// middleware pays off on.
func points() []float32 {
	pts := make([]float32, 60000)
	for i := range pts {
		pts[i] = float32(i) * 0.318309886
	}
	return pts
}

func partition(c *router.Context) {
	handlerRuns.Add(1)
	c.JSON(http.StatusOK, Partition{Points: points()})
}

func stats(c *router.Context) {
	c.JSON(http.StatusOK, Stats{HandlerRuns: handlerRuns.Load()})
}

func unavailable(c *router.Context) {
	c.JSON(http.StatusServiceUnavailable, Failure{Error: "world unavailable"})
}

// blob is a handler that compressed its own payload, as one serving
// pre-compressed files from disk would. It owns the encoding, so the
// middleware passes it through rather than encoding it twice.
func blob(c *router.Context) {
	c.Header("Content-Encoding", "gzip")
	c.Bytes(
		http.StatusOK,
		"application/octet-stream",
		[]byte(strings.Repeat("pretend this is gzip\n", 200)),
	)
}

// ticks streams, and never reaches the size threshold. Flushing settles the
// response uncompressed rather than leaving the client waiting on a buffer
// that will not fill.
func ticks(c *router.Context) {
	sse := c.SSE(http.StatusOK)
	defer sse.Close()
	for i := range 5 {
		if err := sse.Send(router.SSEEvent{
			Event: "tick",
			Data:  fmt.Sprintf("%d", i),
		}); err != nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func main() {
	r := router.New()

	cfg := outputcache.DefaultConfig()
	cfg.Storage = inmemory.New()
	cache := outputcache.New(r, cfg)

	r.Use(cache.Middleware())
	r.Use(compress.Default())

	r.Get("/partition", partition,
		outputcache.WithOutputCache(partitionCacheTTL,
			outputcache.VaryByEncoding(),
		),
		openapi.Summary("A large JSON body, cached and compressed"),
		openapi.Description(
			"Compression is registered inside the output cache, so this "+
				"route is compressed once per entry rather than once per "+
				"request and the cache stores the encoded bytes. That makes "+
				"the encoding part of what the entry is, which is why the "+
				"route also varies by it: without VaryByEncoding a client "+
				"that asked for no compression could be handed an entry it "+
				"cannot read.",
		),
		openapi.Tags("Compression"),
		openapi.ReturnsBody[Partition](http.StatusOK, "The partition"),
	)

	r.Get("/stats", stats,
		openapi.Summary("How many times the cached handler ran"),
		openapi.Description(
			"Fetch /partition under several encodings, then read this: the "+
				"handler ran once per encoding, not once per request.",
		),
		openapi.Tags("Compression"),
		openapi.ReturnsBody[Stats](http.StatusOK, "Handler run count"),
	)

	r.Get("/unavailable", unavailable,
		openapi.Summary("A body too small to be worth compressing"),
		openapi.Description(
			"Under the default 1 KiB threshold. A gzip stream carries about "+
				"eighteen bytes of framing, so compressing this would make "+
				"it bigger.",
		),
		openapi.Tags("Compression"),
		openapi.ReturnsBody[Failure](
			http.StatusServiceUnavailable,
			"The failure",
		),
	)

	r.Get("/blob", blob,
		openapi.Summary("A payload the handler encoded itself"),
		openapi.Description(
			"The handler set Content-Encoding, so the middleware leaves the "+
				"body alone instead of running it through a second "+
				"compressor.",
		),
		openapi.Tags("Compression"),
		openapi.Returns(http.StatusOK, "The blob, gzip-encoded"),
	)

	r.Get("/ticks", ticks,
		openapi.Summary("A stream, which is never held in the buffer"),
		openapi.Description(
			"Flushing settles the response as it stands. A stream that has "+
				"not earned compression is sent uncompressed rather than "+
				"waiting for a threshold it may never reach.",
		),
		openapi.Tags("Compression"),
		openapi.SSEStream[string](http.StatusOK, "Ticks"),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Compression Showcase",
		Version:     "0.1.0",
		Description: "Every branch of the compression middleware, over curl.",
	})
	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	addr := ":8080"
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
