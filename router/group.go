package router

import "net/http"

// Group is a sub-scope of a Router sharing a path prefix and options that
// cascade to every endpoint registered through it.
type Group struct {
	router *Router
	prefix string
	opts   []Option
}

// Group nests a sub-group with an additional prefix. The child inherits the
// parent's options.
func (g *Group) Group(prefix string, opts ...Option) *Group {
	merged := make([]Option, 0, len(g.opts)+len(opts))
	merged = append(merged, g.opts...)
	merged = append(merged, opts...)
	return &Group{
		router: g.router,
		prefix: joinPath(g.prefix, prefix),
		opts:   merged,
	}
}

// Get registers a typed GET handler scoped to this group.
func (g *Group) Get(path string, handler any, opts ...Option) *Endpoint {
	return g.router.register(http.MethodGet, path, handler, g, opts)
}

// Post registers a typed POST handler scoped to this group.
func (g *Group) Post(path string, handler any, opts ...Option) *Endpoint {
	return g.router.register(http.MethodPost, path, handler, g, opts)
}

// Put registers a typed PUT handler scoped to this group.
func (g *Group) Put(path string, handler any, opts ...Option) *Endpoint {
	return g.router.register(http.MethodPut, path, handler, g, opts)
}

// Patch registers a typed PATCH handler scoped to this group.
func (g *Group) Patch(path string, handler any, opts ...Option) *Endpoint {
	return g.router.register(http.MethodPatch, path, handler, g, opts)
}

// Delete registers a typed DELETE handler scoped to this group.
func (g *Group) Delete(path string, handler any, opts ...Option) *Endpoint {
	return g.router.register(http.MethodDelete, path, handler, g, opts)
}

func groupPrefix(g *Group) string {
	if g == nil {
		return ""
	}
	return g.prefix
}
