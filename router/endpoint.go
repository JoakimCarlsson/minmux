package router

import "reflect"

// Endpoint represents a registered route. Fluent methods (Tags, Summary,
// Description) configure OpenAPI metadata and return the endpoint so calls
// can be chained.
type Endpoint struct {
	Method     string
	Path       string
	ParamType  reflect.Type // nil if handler takes no Params struct
	ResultType reflect.Type // the T in (T, error)

	tags        []string
	summary     string
	description string
}

// Tags appends tags to this endpoint's OpenAPI metadata.
func (e *Endpoint) Tags(t ...string) *Endpoint {
	e.tags = append(e.tags, t...)
	return e
}

// Summary sets the short description (one-line) for OpenAPI.
func (e *Endpoint) Summary(s string) *Endpoint {
	e.summary = s
	return e
}

// Description sets the long-form description for OpenAPI.
func (e *Endpoint) Description(d string) *Endpoint {
	e.description = d
	return e
}

// GetTags returns the accumulated tags (group-inherited + endpoint-added).
func (e *Endpoint) GetTags() []string { return e.tags }

// GetSummary returns the configured summary.
func (e *Endpoint) GetSummary() string { return e.summary }

// GetDescription returns the configured description.
func (e *Endpoint) GetDescription() string { return e.description }
