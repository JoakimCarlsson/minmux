package outputcache

import (
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

// routeConfig holds the per-route cache configuration stashed in an
// endpoint's Metadata. The cache middleware reads it back at request time.
type routeConfig struct {
	Duration time.Duration
	Options  []interface{}
	Profile  string
}

// metaKey is the private map key used to store routeConfig in
// router.Endpoint.Metadata. Using a struct type avoids collisions with
// other annotation packages.
type metaKey struct{}

// WithOutputCache is a router.Option that opts a route in to output
// caching with the given TTL and options (VaryByQuery, Tags, etc.).
//
//	r.Get("/products", listProducts,
//	    outputcache.WithOutputCache(time.Minute,
//	        outputcache.VaryByQuery("page"),
//	        outputcache.Tags("products"),
//	    ),
//	)
func WithOutputCache(
	duration time.Duration,
	opts ...interface{},
) router.Option {
	return func(ep *router.Endpoint) {
		ep.Metadata[metaKey{}] = &routeConfig{Duration: duration, Options: opts}
	}
}

// WithCacheProfile is a router.Option that opts a route in to caching
// using a named profile registered on the Cache's Config.Profiles.
//
//	profiles := outputcache.NewProfiles()
//	profiles.Add("aggressive", time.Hour, outputcache.SlidingExpiration())
//	r.Get("/products", listProducts, outputcache.WithCacheProfile("aggressive"))
func WithCacheProfile(name string) router.Option {
	return func(ep *router.Endpoint) {
		ep.Metadata[metaKey{}] = &routeConfig{Profile: name}
	}
}

// routeConfigOf returns the cache configuration attached to ep, or nil if
// the endpoint hasn't opted in.
func routeConfigOf(ep *router.Endpoint) *routeConfig {
	if ep == nil {
		return nil
	}
	if cfg, ok := ep.Metadata[metaKey{}].(*routeConfig); ok {
		return cfg
	}
	return nil
}
