package router

import "net/http"

// Group is a sub-scope of a Router sharing a path prefix and metadata that
// cascades to every endpoint registered through it.
type Group struct {
	router *Router
	prefix string
	tags   []string
}

// Tags appends tags that will cascade to every endpoint in this group.
func (g *Group) Tags(t ...string) *Group {
	g.tags = append(g.tags, t...)
	return g
}

// Group nests a sub-group with an additional prefix. The child inherits the
// parent's tags.
func (g *Group) Group(prefix string) *Group {
	return &Group{
		router: g.router,
		prefix: joinPath(g.prefix, prefix),
		tags:   append([]string(nil), g.tags...),
	}
}

// Get registers a typed GET handler scoped to this group.
func (g *Group) Get(path string, handler any) *Endpoint {
	return g.router.register(http.MethodGet, path, handler, g)
}

// Post registers a typed POST handler scoped to this group.
func (g *Group) Post(path string, handler any) *Endpoint {
	return g.router.register(http.MethodPost, path, handler, g)
}

// Put registers a typed PUT handler scoped to this group.
func (g *Group) Put(path string, handler any) *Endpoint {
	return g.router.register(http.MethodPut, path, handler, g)
}

// Patch registers a typed PATCH handler scoped to this group.
func (g *Group) Patch(path string, handler any) *Endpoint {
	return g.router.register(http.MethodPatch, path, handler, g)
}

// Delete registers a typed DELETE handler scoped to this group.
func (g *Group) Delete(path string, handler any) *Endpoint {
	return g.router.register(http.MethodDelete, path, handler, g)
}

func groupPrefix(g *Group) string {
	if g == nil {
		return ""
	}
	return g.prefix
}

func groupTags(g *Group) []string {
	if g == nil {
		return nil
	}
	return g.tags
}
