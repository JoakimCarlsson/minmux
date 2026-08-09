package compress

import (
	"compress/flate"
	"net/http"
	"strconv"
	"strings"
)

// Encoding names this middleware can produce, in server preference order.
const (
	EncodingGzip    = "gzip"
	EncodingDeflate = "deflate"
)

// supported is the server's own preference order, used to break ties between
// encodings a client weighted equally.
var supported = []string{EncodingGzip, EncodingDeflate}

// Default returns a compression middleware with the default settings: gzip or
// deflate, whichever the client prefers, at the default level, for textual and
// JSON-like responses of at least DefaultMinSize bytes.
func Default() func(http.Handler) http.Handler {
	return New(Options{})
}

// New constructs a compression middleware with the given options.
//
// The middleware negotiates against Accept-Encoding and leaves a response
// alone unless it can improve it: a body the handler already encoded, a media
// type that is already compressed, a partial or empty response, and a body
// under Options.MinSize all pass through untouched. A response that could have
// been compressed gains Vary: Accept-Encoding whether or not it ends up
// compressed, so a shared cache in front of the server keeps the encodings
// apart.
//
// A level outside the range flate accepts is replaced by the default level
// rather than rejected, so a mis-set option degrades instead of panicking.
func New(opts Options) func(http.Handler) http.Handler {
	opts = opts.withDefaults()
	if opts.Level < flate.HuffmanOnly || opts.Level > flate.BestCompression {
		opts.Level = flate.DefaultCompression
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opts.Skip != nil && opts.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			cw := &responseWriter{
				ResponseWriter: w,
				opts:           opts,
				encoding:       Negotiate(r.Header.Get("Accept-Encoding")),
				status:         http.StatusOK,
			}
			defer cw.close()
			next.ServeHTTP(cw, r)
		})
	}
}

// Negotiate picks the encoding to use for an Accept-Encoding header, or "" for
// a client that wants none of what this package produces.
//
// Quality values are honoured, including the explicit refusals — "gzip;q=0"
// rules gzip out — and a wildcard stands in for anything not named. Ties go to
// the server's preference, which is gzip: it is what every client understands
// and what intermediaries handle best.
func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	qualities := parseAcceptEncoding(acceptEncoding)
	wildcard, hasWildcard := qualities["*"]

	best, bestQ := "", 0.0
	for _, encoding := range supported {
		q, ok := qualities[encoding]
		if !ok {
			if !hasWildcard {
				continue
			}
			q = wildcard
		}
		if q > bestQ {
			best, bestQ = encoding, q
		}
	}
	return best
}

// parseAcceptEncoding maps each named encoding to its quality value. Anything
// unparseable is skipped rather than failing the request: a malformed header
// costs the client compression, not its response.
func parseAcceptEncoding(header string) map[string]float64 {
	qualities := make(map[string]float64)
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		q := 1.0
		if key, value, ok := strings.Cut(params, "="); ok &&
			strings.EqualFold(strings.TrimSpace(key), "q") {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		qualities[name] = q
	}
	return qualities
}

// compressible reports whether a media type is one of the configured types.
// An entry beginning with "+" matches a structured syntax suffix; every other
// entry matches a prefix.
func compressible(contentType string, types []string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	media = strings.ToLower(strings.TrimSpace(media))
	if media == "" {
		return false
	}
	for _, t := range types {
		if strings.HasPrefix(t, "+") {
			if strings.HasSuffix(media, t) {
				return true
			}
			continue
		}
		if strings.HasPrefix(media, t) {
			return true
		}
	}
	return false
}
