package compress

import (
	"compress/flate"
	"net/http"
)

// DefaultMinSize is the body size below which compressing is a loss. A gzip
// stream carries about eighteen bytes of framing, so a short error body comes
// out bigger than it went in.
const DefaultMinSize = 1024

// DefaultTypes is the set of content types compressed when Options.Types is
// empty. Each entry is matched as a prefix of the response's media type, so
// "text/" covers every textual subtype. Anything already compressed — images,
// video, archives, and the pre-compressed blobs an application serves itself —
// is deliberately absent: running it through a second compressor spends CPU to
// add bytes.
var DefaultTypes = []string{
	"application/json",
	"application/javascript",
	"application/manifest+json",
	"application/wasm",
	"application/xhtml+xml",
	"application/xml",
	"image/svg+xml",
	"text/",
}

// Options configures the compression middleware.
type Options struct {
	// Level is the compression level, from flate.BestSpeed to
	// flate.BestCompression. Zero selects flate.DefaultCompression.
	Level int

	// MinSize is the number of body bytes a response must reach before it is
	// compressed. Zero selects DefaultMinSize. Negative compresses every
	// eligible response regardless of size.
	//
	// Responses shorter than this are held in memory until the handler
	// finishes, so this is also the middleware's per-request buffer ceiling.
	MinSize int

	// Types is the set of content types to compress, matched as prefixes of
	// the response media type. Empty selects DefaultTypes.
	Types []string

	// Skip decides per-request whether to leave the response alone. Useful
	// for paths that stream, that serve pre-compressed payloads, or that a
	// proxy already handles.
	Skip func(*http.Request) bool
}

// withDefaults fills in the zero values Options documents as defaults.
func (o Options) withDefaults() Options {
	if o.Level == 0 {
		o.Level = flate.DefaultCompression
	}
	if o.MinSize == 0 {
		o.MinSize = DefaultMinSize
	}
	if len(o.Types) == 0 {
		o.Types = DefaultTypes
	}
	return o
}
