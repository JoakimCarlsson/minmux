package router

import (
	"net/http"
	"reflect"
)

// Endpoint represents a registered route. Other packages attach annotation
// data via Metadata using their own key types; the router does not interpret
// any of it.
//
// Middleware is the per-endpoint http.Handler chain applied around the
// typed dispatcher. The first entry is the outermost wrapper (matches
// the Router.Use cascade direction). Populated by the Middleware Option
// and used at registration time; further mutation has no effect.
type Endpoint struct {
	Method     string
	Path       string
	ParamType  reflect.Type // nil if handler takes no Params struct
	Metadata   map[any]any
	Middleware []func(http.Handler) http.Handler
}

// Option configures an Endpoint at registration time. Annotation packages
// (openapi, tracing, auth, etc.) expose functions that return Options to
// stash data into the endpoint's Metadata map.
type Option func(*Endpoint)

// Middleware attaches one or more standard `func(http.Handler) http.Handler`
// middlewares to a single endpoint. The first middleware is the outermost
// wrapper (sees the request first, response last) — same order as
// Router.Use.
//
// Cascades correctly through groups: passing Middleware as a group option
// applies it to every endpoint registered through the group, and a route
// may add its own on top:
//
//	api := r.Group("/api", router.Middleware(auth))
//	api.Get("/users/{id}", getUser, router.Middleware(rateLimit)) // auth -> rateLimit -> handler
func Middleware(mw ...func(http.Handler) http.Handler) Option {
	return func(ep *Endpoint) {
		ep.Middleware = append(ep.Middleware, mw...)
	}
}
