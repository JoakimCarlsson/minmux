package auth

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// Verifier extracts and validates one scheme's credential from the request.
// It is non-failing by contract: return ErrNoCredential when the credential is
// absent (so optional routes fall through to anonymous and multi-scheme OR
// alternatives can be tried), ErrForbidden when the credential is valid but not
// permitted (403), and any other error when a credential is present but invalid
// (401). scopes carries the scopes/roles declared alongside the scheme.
type Verifier func(r *http.Request, scopes []string) (any, error)

// ErrNoCredential signals that the scheme's credential is absent.
var ErrNoCredential = errors.New("auth: no credential")

// ErrForbidden signals an authenticated but unauthorized caller (403).
var ErrForbidden = errors.New("auth: forbidden")

// The problem types the Authenticator sets on the default problems it writes,
// one per failure outcome.
const (
	// TypeUnauthenticated is the type of the 401 for a route that requires a
	// credential the request did not carry.
	TypeUnauthenticated = "unauthenticated"
	// TypeInvalidCredentials is the type of the 401 for a credential that is
	// present but invalid, or for a scheme with no registered verifier.
	TypeInvalidCredentials = "invalid_credentials"
	// TypeForbidden is the type of the 403 for a valid credential without the
	// required permission.
	TypeForbidden = "forbidden"
)

// Authenticator holds the registered verifiers and enforces route security.
type Authenticator struct {
	router    *router.Router
	verifiers map[string]Verifier
	problem   func(*http.Request, *router.ProblemDetails) *router.ProblemDetails
}

// New constructs an Authenticator bound to the router whose endpoint metadata
// it reads, with the verifiers from cfg.
func New(r *router.Router, cfg Config) *Authenticator {
	verifiers := make(map[string]Verifier, len(cfg.Verifiers))
	maps.Copy(verifiers, cfg.Verifiers)
	return &Authenticator{
		router:    r,
		verifiers: verifiers,
		problem:   cfg.Problem,
	}
}

// Middleware enforces each request against the matched endpoint's declared
// security requirements.
func (m *Authenticator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ep := m.router.Match(r)
			if ep == nil {
				next.ServeHTTP(w, r)
				return
			}
			reqs, ok := openapi.SecurityOf(ep)
			if !ok || len(reqs) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			principals, problem := m.authorize(r, reqs)
			if problem != nil {
				writeProblem(w, m.shape(r, problem))
				return
			}
			if len(principals) > 0 {
				r = r.WithContext(withPrincipals(r.Context(), principals))
			}
			next.ServeHTTP(w, r)
		})
	}
}

type outcome int

const (
	satisfied outcome = iota
	absent
	unauthorized
	forbidden
)

// authorize evaluates the OR-list reqs against r. It returns the resolved
// principals of the first satisfied requirement, nil principals and a nil
// problem when anonymous access is allowed, or the default problem to write:
// TypeInvalidCredentials when any credential was invalid, TypeForbidden when
// a credential lacked the permission, and TypeUnauthenticated otherwise.
func (m *Authenticator) authorize(
	r *http.Request,
	reqs []openapi.SecurityRequirement,
) (map[string]any, *router.ProblemDetails) {
	var sawUnauthorized, sawForbidden, anonymousOK bool
	for _, req := range reqs {
		if len(req) == 0 {
			anonymousOK = true
			continue
		}
		principals, oc := m.trySatisfy(r, req)
		switch oc {
		case satisfied:
			return principals, nil
		case unauthorized:
			sawUnauthorized = true
		case forbidden:
			sawForbidden = true
		case absent:
		}
	}
	if sawUnauthorized {
		return nil, withType(
			router.Unauthorized("invalid or missing credentials"),
			TypeInvalidCredentials,
		)
	}
	if anonymousOK {
		return nil, nil
	}
	if sawForbidden {
		return nil, withType(
			router.Forbidden("insufficient permissions"),
			TypeForbidden,
		)
	}
	return nil, withType(
		router.Unauthorized("authentication required"),
		TypeUnauthenticated,
	)
}

// withType sets typ as p's Type and returns p.
func withType(p *router.ProblemDetails, typ string) *router.ProblemDetails {
	p.Type = typ
	return p
}

// shape returns the problem to write in place of the default p: the result of
// the Config.Problem hook when one is set and returns non-nil, otherwise p.
// A hook result with a zero Status is written with p's status.
func (m *Authenticator) shape(
	r *http.Request,
	p *router.ProblemDetails,
) *router.ProblemDetails {
	if m.problem == nil {
		return p
	}
	status := p.Status
	shaped := m.problem(r, p)
	if shaped == nil {
		return p
	}
	if shaped.Status != 0 {
		return shaped
	}
	fixed := *shaped
	fixed.Status = status
	return &fixed
}

func (m *Authenticator) trySatisfy(
	r *http.Request,
	req openapi.SecurityRequirement,
) (map[string]any, outcome) {
	principals := make(map[string]any, len(req))
	for scheme, scopes := range req {
		v := m.verifiers[scheme]
		if v == nil {
			return nil, unauthorized
		}
		p, err := v(r, scopes)
		if err == nil {
			principals[scheme] = p
			continue
		}
		if errors.Is(err, ErrNoCredential) {
			return nil, absent
		}
		if errors.Is(err, ErrForbidden) {
			return nil, forbidden
		}
		return nil, unauthorized
	}
	return principals, satisfied
}

func writeProblem(w http.ResponseWriter, pd *router.ProblemDetails) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pd.Status)
	_ = json.NewEncoder(w).Encode(pd)
}

type principalsKeyType struct{}

var principalsKey principalsKeyType

func withPrincipals(
	ctx context.Context,
	p map[string]any,
) context.Context {
	return context.WithValue(ctx, principalsKey, p)
}

func principalsFrom(ctx context.Context) map[string]any {
	p, _ := ctx.Value(principalsKey).(map[string]any)
	return p
}

// PrincipalFor returns the principal resolved for a specific scheme.
func PrincipalFor[T any](ctx context.Context, scheme string) (T, bool) {
	var zero T
	v, ok := principalsFrom(ctx)[scheme]
	if !ok {
		return zero, false
	}
	tv, ok := v.(T)
	return tv, ok
}

// Principal returns the sole resolved principal, for the common single-scheme
// case. It returns false when zero or multiple schemes authenticated.
func Principal[T any](ctx context.Context) (T, bool) {
	var zero T
	p := principalsFrom(ctx)
	if len(p) != 1 {
		return zero, false
	}
	for _, v := range p {
		tv, ok := v.(T)
		return tv, ok
	}
	return zero, false
}
