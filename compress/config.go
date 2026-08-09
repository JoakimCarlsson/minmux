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
// empty. Anything already compressed — images, video, archives, and the
// pre-compressed blobs an application serves itself — is deliberately absent:
// running it through a second compressor spends CPU to add bytes.
//
// An entry beginning with "+" matches a structured syntax suffix (RFC 6839)
// rather than a prefix, so "+json" covers application/problem+json,
// application/ld+json, application/vnd.api+json and every other name ending
// that way. Without it each would have to be listed, and the one that matters
// most here is minmux's own: router serves its RFC 9457 error bodies as
// application/problem+json.
var DefaultTypes = []string{
	"application/json",
	"application/javascript",
	"application/wasm",
	"application/xml",
	"text/",
	"+json",
	"+xml",
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

	// Types is the set of content types to compress. An entry is matched as
	// a prefix of the response media type, except one beginning with "+",
	// which matches a structured syntax suffix: "+json" covers
	// application/problem+json and its kind. Empty selects DefaultTypes.
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
