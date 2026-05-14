package router

import (
	"fmt"
	"net/http"
	"strings"
)

// Router is the entry point of a minmux app. It implements http.Handler.
type Router struct {
	mux        *http.ServeMux
	middleware []func(http.Handler) http.Handler
	endpoints  []*Endpoint
	codec      Codec
}

// Option configures a Router at construction time.
type Option func(*Router)

// WithCodec replaces the default JSON codec.
func WithCodec(c Codec) Option {
	return func(r *Router) { r.codec = c }
}

// New constructs a Router.
func New(opts ...Option) *Router {
	r := &Router{
		mux:   http.NewServeMux(),
		codec: jsonCodec{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ServeHTTP makes Router an http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var h http.Handler = r.mux
	for i := len(r.middleware) - 1; i >= 0; i-- {
		h = r.middleware[i](h)
	}
	h.ServeHTTP(w, req)
}

// Use registers http.Handler-style middleware that wraps every request.
func (r *Router) Use(mw ...func(http.Handler) http.Handler) {
	r.middleware = append(r.middleware, mw...)
}

// Endpoints returns every typed endpoint registered on this router. Used by
// openapi spec generation. Raw handlers registered via HandleFunc / Handle
// are not included.
func (r *Router) Endpoints() []*Endpoint {
	return r.endpoints
}

// Get registers a typed GET handler.
func (r *Router) Get(path string, handler any) *Endpoint {
	return r.register(http.MethodGet, path, handler, nil)
}

// Post registers a typed POST handler.
func (r *Router) Post(path string, handler any) *Endpoint {
	return r.register(http.MethodPost, path, handler, nil)
}

// Put registers a typed PUT handler.
func (r *Router) Put(path string, handler any) *Endpoint {
	return r.register(http.MethodPut, path, handler, nil)
}

// Patch registers a typed PATCH handler.
func (r *Router) Patch(path string, handler any) *Endpoint {
	return r.register(http.MethodPatch, path, handler, nil)
}

// Delete registers a typed DELETE handler.
func (r *Router) Delete(path string, handler any) *Endpoint {
	return r.register(http.MethodDelete, path, handler, nil)
}

// Group creates a route group with a shared prefix and cascading metadata.
func (r *Router) Group(prefix string) *Group {
	return &Group{router: r, prefix: prefix}
}

// HandleFunc registers a raw http.HandlerFunc for the given method and path,
// bypassing the typed-handler dispatcher. Routes registered this way do not
// appear in Endpoints() and are not included in the OpenAPI spec.
func (r *Router) HandleFunc(method, path string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+path, h)
}

// Handle registers a raw http.Handler. See HandleFunc.
func (r *Router) Handle(method, path string, h http.Handler) {
	r.mux.Handle(method+" "+path, h)
}

func (r *Router) register(
	method, path string,
	handler any,
	g *Group,
) *Endpoint {
	full := joinPath(groupPrefix(g), path)
	dispatch, info, err := buildDispatcher(handler, r.codec)
	if err != nil {
		panic(fmt.Sprintf("minmux: %s %s: %v", method, full, err))
	}
	ep := &Endpoint{
		Method:     method,
		Path:       full,
		ParamType:  info.paramType,
		ResultType: info.resultType,
		tags:       append([]string(nil), groupTags(g)...),
	}
	r.endpoints = append(r.endpoints, ep)
	r.mux.HandleFunc(method+" "+full, dispatch)
	return ep
}

func joinPath(prefix, path string) string {
	prefix = strings.TrimRight(prefix, "/")
	if path == "" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return prefix + path
}
