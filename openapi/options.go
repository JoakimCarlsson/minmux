package openapi

import (
	"reflect"

	"github.com/joakimcarlsson/minmux/router"
)

// endpointMeta is the OpenAPI annotation data attached to a router.Endpoint
// via options. Stored in Endpoint.Metadata keyed by metaKey{}.
type endpointMeta struct {
	Summary     string
	Description string
	Tags        []string
	Responses   []ResponseDecl
}

// streamKind classifies how a ResponseDecl should be emitted.
type streamKind int

const (
	// streamNone is the default: a regular response, emitted under
	// application/json with schema:.
	streamNone streamKind = iota
	// streamSequential is a sequential media-type stream (jsonl, ndjson,
	// json-seq, geo+json-seq). Emits itemSchema per declared content type.
	streamSequential
	// streamSSE is text/event-stream. Emits the canonical SSE event schema
	// from OAS 3.2 §4.14.4, with the data field carrying ItemType when set.
	streamSSE
	// streamMultipartMixed is multipart/mixed (or any positional multipart).
	// Emits itemSchema + itemEncoding, optionally with prefixEncoding for a
	// fixed prefix.
	streamMultipartMixed
)

// ResponseDecl is a single explicit response declaration. BodyType is nil
// when the response has no body (204 No Content, 304 Not Modified, etc.).
//
// For streaming responses (StreamKind != streamNone), ItemType holds the
// per-item Go type that should be lifted into itemSchema, and ContentTypes
// names the media types the handler may emit.
type ResponseDecl struct {
	Status       int
	Description  string
	BodyType     reflect.Type
	StreamKind   streamKind
	ContentTypes []string
	ItemType     reflect.Type
	ItemEncoding *Encoding
	PrefixParts  []*Encoding
}

// metaKey is the private key used to store endpointMeta in
// router.Endpoint.Metadata.
type metaKey struct{}

func writeMeta(ep *router.Endpoint) *endpointMeta {
	if m, ok := ep.Metadata[metaKey{}].(*endpointMeta); ok {
		return m
	}
	m := &endpointMeta{}
	ep.Metadata[metaKey{}] = m
	return m
}

func readMeta(ep *router.Endpoint) *endpointMeta {
	if m, ok := ep.Metadata[metaKey{}].(*endpointMeta); ok {
		return m
	}
	return &endpointMeta{}
}

// Summary sets the operation's short one-line description.
func Summary(s string) router.Option {
	return func(ep *router.Endpoint) { writeMeta(ep).Summary = s }
}

// Description sets the operation's long-form description.
func Description(s string) router.Option {
	return func(ep *router.Endpoint) { writeMeta(ep).Description = s }
}

// Tags appends tags used to group operations in the rendered docs.
func Tags(t ...string) router.Option {
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Tags = append(m.Tags, t...)
	}
}

// Returns declares a response with a status code and no body. Use this for
// 204 No Content, 304 Not Modified, redirects, and any other status that
// has no payload. Pass an empty description to use the standard HTTP
// status text.
func Returns(status int, description string) router.Option {
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Responses = append(m.Responses, ResponseDecl{
			Status:      status,
			Description: description,
		})
	}
}

// ReturnsBody declares a response with a status code and a typed JSON body.
// Pass an empty description to use the standard HTTP status text.
func ReturnsBody[T any](status int, description string) router.Option {
	bodyType := reflect.TypeFor[T]()
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Responses = append(m.Responses, ResponseDecl{
			Status:      status,
			Description: description,
			BodyType:    bodyType,
		})
	}
}

// StreamsBody declares a streaming response carrying a sequential media type.
// Each Content-Type in contentTypes is emitted with itemSchema describing T.
// If no content types are given the default is application/jsonl.
//
// Typical content types:
//   - application/jsonl
//   - application/x-ndjson
//   - application/json-seq
//   - application/geo+json-seq
//
// Use SSEStream for text/event-stream and MultipartMixedStream for
// multipart/mixed, since those need their own structural metadata.
func StreamsBody[T any](
	status int,
	description string,
	contentTypes ...string,
) router.Option {
	itemType := reflect.TypeFor[T]()
	cts := append([]string(nil), contentTypes...)
	if len(cts) == 0 {
		cts = []string{"application/jsonl"}
	}
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Responses = append(m.Responses, ResponseDecl{
			Status:       status,
			Description:  description,
			StreamKind:   streamSequential,
			ContentTypes: cts,
			ItemType:     itemType,
		})
	}
}

// SSEStream declares a Server-Sent Events response. The generated spec uses
// the canonical SSE event schema from OAS 3.2 §4.14.4 ({data, event, id,
// retry}); when T is not the zero type, the data property is annotated with
// contentMediaType: application/json and contentSchema describing T, so
// clients know how to parse the data field.
//
// Pass a struct (or any non-empty type) for T when SSE data carries JSON;
// pass struct{}{} or any{} when data is opaque text.
func SSEStream[T any](status int, description string) router.Option {
	itemType := reflect.TypeFor[T]()
	if itemType.Kind() == reflect.Struct && itemType.NumField() == 0 {
		itemType = nil
	}
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Responses = append(m.Responses, ResponseDecl{
			Status:       status,
			Description:  description,
			StreamKind:   streamSSE,
			ContentTypes: []string{"text/event-stream"},
			ItemType:     itemType,
		})
	}
}

// MultipartMixedOption configures a MultipartMixedStream declaration.
type MultipartMixedOption func(*ResponseDecl)

// WithItemContentType sets the Content-Type used in itemEncoding (the
// content type each repeating part will carry). Defaults to
// application/octet-stream when unset.
func WithItemContentType(ct string) MultipartMixedOption {
	return func(d *ResponseDecl) {
		if d.ItemEncoding == nil {
			d.ItemEncoding = &Encoding{}
		}
		d.ItemEncoding.ContentType = ct
	}
}

// WithItemEncoding sets the full Encoding Object for repeating parts,
// overriding any WithItemContentType call.
func WithItemEncoding(e *Encoding) MultipartMixedOption {
	return func(d *ResponseDecl) { d.ItemEncoding = e }
}

// WithPrefixParts declares a fixed sequence of leading parts. Each Encoding
// describes the contentType (and optional headers) for the part at its
// position. Useful for "metadata-then-stream" multipart layouts.
func WithPrefixParts(parts ...*Encoding) MultipartMixedOption {
	return func(d *ResponseDecl) {
		d.PrefixParts = append(d.PrefixParts, parts...)
	}
}

// MultipartMixedStream declares a multipart/mixed streaming response. Each
// repeating part is described by itemSchema (built from T) and itemEncoding
// (set via WithItemContentType / WithItemEncoding).
func MultipartMixedStream[T any](
	status int,
	description string,
	opts ...MultipartMixedOption,
) router.Option {
	itemType := reflect.TypeFor[T]()
	if itemType.Kind() == reflect.Struct && itemType.NumField() == 0 {
		itemType = nil
	}
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		decl := ResponseDecl{
			Status:       status,
			Description:  description,
			StreamKind:   streamMultipartMixed,
			ContentTypes: []string{"multipart/mixed"},
			ItemType:     itemType,
		}
		for _, opt := range opts {
			opt(&decl)
		}
		m.Responses = append(m.Responses, decl)
	}
}
