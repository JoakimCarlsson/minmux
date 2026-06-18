// Package auth turns minmux's OpenAPI security annotations into runtime
// enforcement. An app supplies one Verifier per security scheme; the
// Authenticator's middleware reads the security requirements already declared on
// each route via openapi.Security / OptionalSecurity / NoSecurity and enforces
// them, resolving the caller's principal into the request context.
//
// Authentication is non-failing (a Verifier just resolves a credential to a
// principal) and authorization is the declarative gate: openapi.Security makes a
// scheme required, openapi.OptionalSecurity allows anonymous access, and
// openapi.NoSecurity (or no annotation) leaves a route unguarded. Missing or
// invalid credentials yield 401; a Verifier reporting ErrForbidden yields 403.
//
//	authn := auth.New(r, auth.Config{
//	    Verifiers: map[string]auth.Verifier{"bearerAuth": verifyBearer},
//	})
//	r.Use(authn.Middleware())
//
//	r.Get("/me", handler, openapi.Security("bearerAuth"))
//	// in the handler:
//	user, _ := auth.Principal[string](c.Ctx())
package auth
