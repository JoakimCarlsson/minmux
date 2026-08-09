// Package compress provides a response compression middleware for any
// http.Handler.
//
// The middleware is independent of minmux/router — it operates on the
// standard http.Handler interface and can be used with any Go HTTP server.
// With minmux:
//
//	r := router.New()
//	r.Use(compress.Default())
//
// It negotiates gzip or deflate against Accept-Encoding and compresses only
// what it can improve: textual and JSON-like media types — including the
// structured syntax suffixes, so minmux's own application/problem+json error
// bodies are covered — bodies of at least DefaultMinSize bytes, and responses
// the handler has not already encoded itself. To tune any of that:
//
//	r.Use(compress.New(compress.Options{
//	    Level:   flate.BestSpeed,
//	    MinSize: 4096,
//	    Types:   []string{"application/json"},
//	    Skip:    func(r *http.Request) bool {
//	        return strings.HasPrefix(r.URL.Path, "/api/tiles/")
//	    },
//	}))
//
// # Ordering against the output cache
//
// Registered inside minmux/outputcache — that is, added with Use after the
// cache's own middleware — a cached route is compressed once per entry rather
// than once per request, and the cache stores the encoded bytes. That is the
// cheaper arrangement, and it makes the encoding part of what the entry is:
// pair it with outputcache.VaryByEncoding on those routes, or a client that
// asked for no compression can be handed an entry it cannot read.
//
// Registered outside the cache, entries stay uncompressed and every hit is
// re-encoded on the way out. That costs CPU per request but keeps one entry
// per route, which is the better trade when bodies are small or the cache is
// shared with clients whose Accept-Encoding varies widely.
package compress
