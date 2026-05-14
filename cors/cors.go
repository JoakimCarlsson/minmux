package cors

import (
	"net/http"
	"strconv"
	"strings"
)

// Options configures the CORS middleware.
type Options struct {
	// AllowOrigins is the list of origins allowed. Each entry is matched
	// case-insensitively against the request Origin header. Supported
	// forms:
	//   - "*"                     — any origin (incompatible with credentials)
	//   - "https://app.com"       — exact match
	//   - "*.example.com"         — subdomain wildcard (matches any subdomain)
	AllowOrigins []string

	// AllowOriginFunc decides per-request whether an origin is allowed.
	// When set, AllowOrigins is ignored.
	AllowOriginFunc func(origin string) bool

	// AllowMethods is the list of HTTP methods reported on preflight
	// responses. Defaults to GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD.
	AllowMethods []string

	// AllowHeaders is the list of request headers reported as allowed on
	// preflight responses. Use ["*"] to allow any.
	AllowHeaders []string

	// ExposeHeaders is the list of response headers the browser exposes
	// to JavaScript on the actual response.
	ExposeHeaders []string

	// AllowCredentials sets Access-Control-Allow-Credentials. When true,
	// the actual request origin is echoed back instead of "*" (browsers
	// reject "*" with credentials).
	AllowCredentials bool

	// MaxAge is the number of seconds browsers may cache the preflight
	// response. Zero omits the header.
	MaxAge int
}

// Default returns a permissive CORS middleware suitable for development:
// allow any origin, all common methods, any request header, no credentials.
// Tighten for production.
func Default() func(http.Handler) http.Handler {
	return New(Options{
		AllowOrigins: []string{"*"},
		AllowMethods: defaultMethods,
		AllowHeaders: []string{"*"},
	})
}

// New constructs a CORS middleware with the given options.
func New(opts Options) func(http.Handler) http.Handler {
	if len(opts.AllowMethods) == 0 {
		opts.AllowMethods = defaultMethods
	}

	allowMethods := strings.Join(opts.AllowMethods, ", ")
	allowHeaders := strings.Join(opts.AllowHeaders, ", ")
	exposeHeaders := strings.Join(opts.ExposeHeaders, ", ")
	maxAge := ""
	if opts.MaxAge > 0 {
		maxAge = strconv.Itoa(opts.MaxAge)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, allowValue := matchOrigin(origin, &opts)
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allowValue)
			h.Add("Vary", "Origin")
			if opts.AllowCredentials && allowValue != "*" {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if exposeHeaders != "" {
				h.Set("Access-Control-Expose-Headers", exposeHeaders)
			}

			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", allowMethods)
				if allowHeaders != "" {
					h.Set("Access-Control-Allow-Headers", allowHeaders)
				}
				if maxAge != "" {
					h.Set("Access-Control-Max-Age", maxAge)
				}
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var defaultMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
	http.MethodHead,
}

// matchOrigin returns (allowed, valueToSendInAllowOriginHeader).
//
// When credentials are allowed and the configured list contains "*", the
// browser-compatible value is the actual request origin, not "*".
func matchOrigin(origin string, opts *Options) (bool, string) {
	if opts.AllowOriginFunc != nil {
		if opts.AllowOriginFunc(origin) {
			return true, origin
		}
		return false, ""
	}

	for _, allowed := range opts.AllowOrigins {
		switch {
		case allowed == "*":
			if opts.AllowCredentials {
				return true, origin
			}
			return true, "*"
		case strings.EqualFold(allowed, origin):
			return true, origin
		case strings.HasPrefix(allowed, "*."):
			suffix := allowed[1:]
			if strings.HasSuffix(
				strings.ToLower(origin),
				strings.ToLower(suffix),
			) {
				return true, origin
			}
		}
	}
	return false, ""
}
