package router

import "reflect"

// Endpoint represents a registered route. Other packages attach annotation
// data via Metadata using their own key types; the router does not interpret
// any of it.
type Endpoint struct {
	Method    string
	Path      string
	ParamType reflect.Type // nil if handler takes no Params struct
	Metadata  map[any]any
}

// Option configures an Endpoint at registration time. Annotation packages
// (openapi, tracing, auth, etc.) expose functions that return Options to
// stash data into the endpoint's Metadata map.
type Option func(*Endpoint)
