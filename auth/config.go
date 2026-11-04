package auth

import (
	"net/http"

	"github.com/joakimcarlsson/minmux/router"
)

// Config configures an Authenticator.
type Config struct {
	// Verifiers maps each security scheme name to the function that validates
	// that scheme's credential. A route declaring openapi.Security("x") is
	// enforced by Verifiers["x"]; a scheme with no registered verifier can never
	// be satisfied (401).
	Verifiers map[string]Verifier

	// Problem, when set, decides the problem written for every 401 and 403
	// the Authenticator answers. It receives the request and the default
	// problem, whose Type tells the outcomes apart: TypeUnauthenticated for
	// a missing credential, TypeInvalidCredentials for an invalid one and
	// TypeForbidden for a credential without the permission. The status of
	// the returned ProblemDetails is the status written, or the default
	// status when it is zero, with Content-Type application/problem+json;
	// returning nil writes the default problem.
	Problem func(r *http.Request, p *router.ProblemDetails) *router.ProblemDetails
}
