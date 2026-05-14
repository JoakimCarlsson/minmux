package router

import (
	"fmt"
	"net/http"
	"strings"
)

// defaultMaxMultipartMemory matches http.defaultMaxMemory: the in-memory
// buffer cap used when parsing multipart/form-data and the read cap used for
// raw []byte body fields. Files larger than this are spilled to a temp file
// by mime/multipart.
const defaultMaxMultipartMemory int64 = 32 << 20

// Router is the entry point of a minmux app. It implements http.Handler.
type Router struct {
	mux                *http.ServeMux
	middleware         []func(http.Handler) http.Handler
	endpoints          []*Endpoint
	codec              Codec
	maxMultipartMemory int64
}

// RouterOption configures a Router at construction time.
type RouterOption func(*Router)

// WithCodec replaces the default JSON codec.
func WithCodec(c Codec) RouterOption {
	return func(r *Router) { r.codec = c }
}

// WithMaxMultipartMemory caps the in-memory buffer used when parsing
// multipart/form-data bodies and the read limit applied to []byte body
// fields. Defaults to 32 MiB. Larger multipart parts are spilled to a
// temporary file on disk by mime/multipart.
func WithMaxMultipartMemory(n int64) RouterOption {
	return func(r *Router) { r.maxMultipartMemory = n }
}

// New constructs a Router.
func New(opts ...RouterOption) *Router {
	r := &Router{
		mux:                http.NewServeMux(),
		codec:              jsonCodec{},
		maxMultipartMemory: defaultMaxMultipartMemory,
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
// annotation consumers (e.g. openapi). Raw handlers registered via HandleFunc
// or Handle are not included.
func (r *Router) Endpoints() []*Endpoint {
	return r.endpoints
}

// Match returns the endpoint that would handle the request, or nil if the
// request would 404 or matches only a raw handler. Used by annotation
// consumers (e.g. outputcache) that need to look up per-endpoint metadata
// from an incoming request.
func (r *Router) Match(req *http.Request) *Endpoint {
	_, pattern := r.mux.Handler(req)
	if pattern == "" {
		return nil
	}
	for _, ep := range r.endpoints {
		if ep.Method+" "+ep.Path == pattern {
			return ep
		}
	}
	return nil
}

// Get registers a typed GET handler with optional annotations.
func (r *Router) Get(path string, handler any, opts ...Option) *Endpoint {
	return r.register(http.MethodGet, path, handler, nil, opts)
}

// Post registers a typed POST handler with optional annotations.
func (r *Router) Post(path string, handler any, opts ...Option) *Endpoint {
	return r.register(http.MethodPost, path, handler, nil, opts)
}

// Put registers a typed PUT handler with optional annotations.
func (r *Router) Put(path string, handler any, opts ...Option) *Endpoint {
	return r.register(http.MethodPut, path, handler, nil, opts)
}

// Patch registers a typed PATCH handler with optional annotations.
func (r *Router) Patch(path string, handler any, opts ...Option) *Endpoint {
	return r.register(http.MethodPatch, path, handler, nil, opts)
}

// Delete registers a typed DELETE handler with optional annotations.
func (r *Router) Delete(path string, handler any, opts ...Option) *Endpoint {
	return r.register(http.MethodDelete, path, handler, nil, opts)
}

// Group creates a route group with a shared prefix. Options passed here
// apply to every endpoint registered through the group.
func (r *Router) Group(prefix string, opts ...Option) *Group {
	return &Group{router: r, prefix: prefix, opts: opts}
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
	opts []Option,
) *Endpoint {
	full := joinPath(groupPrefix(g), path)
	dispatch, info, err := buildDispatcher(handler, bindConfig{
		codec:              r.codec,
		maxMultipartMemory: r.maxMultipartMemory,
	})
	if err != nil {
		panic(fmt.Sprintf("minmux: %s %s: %v", method, full, err))
	}
	ep := &Endpoint{
		Method:    method,
		Path:      full,
		ParamType: info.paramType,
		Metadata:  map[any]any{},
	}
	if g != nil {
		for _, opt := range g.opts {
			opt(ep)
		}
	}
	for _, opt := range opts {
		opt(ep)
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
