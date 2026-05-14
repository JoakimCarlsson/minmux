package openapi

import (
	"reflect"

	"github.com/joakimcarlsson/minmux/router"
)

// endpointMeta is the OpenAPI annotation data attached to a router.Endpoint
// via options. Stored in Endpoint.Metadata keyed by metaKey{}.
//
// Security accumulates the alternative Security Requirement Objects this
// operation accepts. SecurityOverride flags an explicit clear (NoSecurity),
// which causes buildOperation to emit "security": [] even when Security is
// empty so that the operation overrides any document-level default.
type endpointMeta struct {
	Summary          string
	Description      string
	Deprecated       bool
	Tags             []string
	Responses        []ResponseDecl
	Security         []SecurityRequirement
	SecurityOverride bool
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

// Deprecated marks the operation as deprecated. Renderers (Scalar, Swagger
// UI) typically style deprecated endpoints with a strikethrough and a
// banner; code generators may emit annotations on the corresponding
// client method.
func Deprecated() router.Option {
	return func(ep *router.Endpoint) { writeMeta(ep).Deprecated = true }
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

// Security adds one Security Requirement Object naming a single scheme.
// Multiple Security calls on the same endpoint accumulate as alternatives
// (OR-combined): a request satisfying any one of them is authorized.
// Scopes apply when the named scheme is of type oauth2 or openIdConnect;
// for other types they convey role names.
//
// When used as a group option, the requirement cascades to every endpoint
// registered through that group. Use NoSecurity on an individual endpoint
// to override an inherited default.
func Security(scheme string, scopes ...string) router.Option {
	req := SecurityRequirement{scheme: append([]string{}, scopes...)}
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Security = append(m.Security, req)
	}
}

// SecurityAll adds one Security Requirement Object listing multiple
// schemes that must all be satisfied together (AND-combined within the
// single requirement). Use this when a request must present, for example,
// both an API key and a signature header.
//
// Subsequent Security / SecurityAll / OptionalSecurity calls accumulate
// as additional OR alternatives.
func SecurityAll(req SecurityRequirement) router.Option {
	copyReq := make(SecurityRequirement, len(req))
	for k, v := range req {
		copyReq[k] = append([]string{}, v...)
	}
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Security = append(m.Security, copyReq)
	}
}

// OptionalSecurity appends the empty Security Requirement Object ({}),
// per OAS 3.2 §4.30 making anonymous access an allowed alternative
// alongside any other declared requirements.
func OptionalSecurity() router.Option {
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Security = append(m.Security, SecurityRequirement{})
	}
}

// NoSecurity emits "security": [] on the operation, clearing any
// document-level or group-level default and explicitly declaring the
// endpoint as unauthenticated. Any Security / SecurityAll /
// OptionalSecurity options previously applied to the endpoint are
// discarded.
func NoSecurity() router.Option {
	return func(ep *router.Endpoint) {
		m := writeMeta(ep)
		m.Security = nil
		m.SecurityOverride = true
	}
}

// BasicAuth returns an HTTP Basic Security Scheme (RFC 7617).
func BasicAuth(description string) *SecurityScheme {
	return &SecurityScheme{
		Type:        "http",
		Scheme:      "basic",
		Description: description,
	}
}

// BearerAuth returns an HTTP Bearer Security Scheme (RFC 6750). The
// bearerFormat is a free-form hint (commonly "JWT"); pass "" to omit it.
func BearerAuth(bearerFormat, description string) *SecurityScheme {
	return &SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: bearerFormat,
		Description:  description,
	}
}

// APIKey returns an API key Security Scheme. The in argument selects the
// transport: "header", "query", or "cookie".
func APIKey(in, name, description string) *SecurityScheme {
	return &SecurityScheme{
		Type:        "apiKey",
		In:          in,
		Name:        name,
		Description: description,
	}
}

// OAuth2Scheme returns an OAuth2 Security Scheme. The caller builds the
// OAuthFlows directly so each flow can carry the URL set it requires
// (deviceAuthorization is OAS 3.2's new flow).
func OAuth2Scheme(flows *OAuthFlows, description string) *SecurityScheme {
	return &SecurityScheme{
		Type:        "oauth2",
		Flows:       flows,
		Description: description,
	}
}

// OpenIDConnect returns an OpenID Connect Discovery Security Scheme. The
// url MUST point at the provider's well-known configuration document.
func OpenIDConnect(url, description string) *SecurityScheme {
	return &SecurityScheme{
		Type:             "openIdConnect",
		OpenIDConnectURL: url,
		Description:      description,
	}
}

// MutualTLS returns a mutual-TLS Security Scheme (OAS 3.1+; client cert
// auth). No additional fields beyond the description are required.
func MutualTLS(description string) *SecurityScheme {
	return &SecurityScheme{
		Type:        "mutualTLS",
		Description: description,
	}
}
