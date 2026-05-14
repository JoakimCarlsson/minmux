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

// ResponseDecl is a single explicit response declaration. BodyType is nil
// when the response has no body (204 No Content, 304 Not Modified, etc.).
type ResponseDecl struct {
	Status      int
	Description string
	BodyType    reflect.Type
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
