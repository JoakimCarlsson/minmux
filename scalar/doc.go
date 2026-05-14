// Package scalar serves the Scalar API Reference UI as an http.HandlerFunc,
// configured to render an OpenAPI document fetched from a URL.
//
// Scalar is a JavaScript-rendered reference UI that supports OpenAPI 3.x,
// including the OAS 3.2 fields emitted by the openapi package. The handler
// returns a single HTML page that embeds Scalar from a CDN and points it
// at the spec URL the caller supplies — typically the same-origin path
// used by openapi.Generator.Handler.
//
//	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))
//	r.HandleFunc(http.MethodGet, "/docs",         scalar.Handler("/openapi.json"))
//
// The module has zero dependencies beyond the standard library.
package scalar
