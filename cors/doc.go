// Package cors provides a CORS middleware for any http.Handler.
//
// The middleware is independent of minmux/router — it operates on the
// standard http.Handler interface and can be used with any Go HTTP server.
// With minmux:
//
//	r := router.New()
//	r.Use(cors.Default())
//
// For production, configure explicit origins:
//
//	r.Use(cors.New(cors.Options{
//	    AllowOrigins:     []string{"https://app.example.com", "*.staging.example.com"},
//	    AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch},
//	    AllowHeaders:     []string{"Authorization", "Content-Type"},
//	    AllowCredentials: true,
//	    MaxAge:           3600,
//	}))
package cors
