// Package router is the core of minmux: an ASP.NET-Minimal-API-inspired
// HTTP framework for Go built on net/http ServeMux pattern routing.
//
// The handler signature is the contract: the framework binds parameters,
// dispatches to the typed handler, and serializes the typed result.
// OpenAPI metadata is derived from the same signature, eliminating
// duplicate type declarations.
package router
