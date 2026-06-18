# Authentication

The [Security](../security.md) page covers the OpenAPI *annotations* —
`Security`, `OptionalSecurity`, `NoSecurity` — that describe what a route
requires. The `auth` package turns those same annotations into runtime
**enforcement**: you register one `Verifier` per scheme, and a single
middleware reads each matched route's declared requirements and applies them.
One declaration drives both the spec and the gate, so they can't drift.

It mirrors ASP.NET Core's split: **authentication is non-failing** (a `Verifier`
just resolves a credential to a principal) and **authorization is the
declarative gate** (the `Security` annotation decides required vs optional vs
public). Missing or invalid credentials yield `401`; a `Verifier` reporting
`ErrForbidden` yields `403`.

## Install

```bash
go get github.com/joakimcarlsson/minmux/auth
```

## Wiring

```go
authn := auth.New(r, auth.Config{
    Verifiers: map[string]auth.Verifier{
        "bearerAuth": verifyBearer,
    },
})
r.Use(authn.Middleware())
```

Register the matching scheme on the generator so it appears in the spec and
Scalar UI (see [Security](../security.md)):

```go
gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
    "bearerAuth": openapi.BearerAuth("JWT", "Bearer JWT from the IdP"),
}
```

## The Verifier

A `Verifier` extracts and validates one scheme's credential. It is non-failing
by contract — the return value (not a panic or a written response) tells the
middleware what to do:

```go
type Verifier func(r *http.Request, scopes []string) (principal any, err error)
```

| Return | Meaning | Effect |
|---|---|---|
| `(principal, nil)` | credential present and valid | principal stored in context; requirement satisfied |
| `(_, auth.ErrNoCredential)` | credential absent | optional routes fall through to anonymous; required routes → 401 |
| `(_, auth.ErrForbidden)` | valid but not permitted (scope/role) | `403` |
| `(_, other error)` | credential present but invalid | `401` |

```go
func verifyBearer(r *http.Request, _ []string) (any, error) {
    token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
    if !ok || token == "" {
        return nil, auth.ErrNoCredential
    }
    claims, err := parseJWT(token) // your validator
    if err != nil {
        return nil, err
    }
    return claims.Subject, nil
}
```

## Per-route behavior

The annotation on the route decides enforcement; no per-route middleware:

```go
r.Get("/public", public, openapi.NoSecurity())               // always open
r.Get("/me", me, openapi.Security("bearerAuth"))             // required → 401 without a valid token
r.Get("/maybe", maybe,                                        // optional
    openapi.OptionalSecurity(),
    openapi.Security("bearerAuth"),
)
```

| Annotation | No credential | Valid credential | Invalid credential |
|---|---|---|---|
| `NoSecurity()` / none | pass | pass | pass |
| `Security(scheme)` | 401 | pass (principal set) | 401 |
| `OptionalSecurity()` + `Security(scheme)` | pass (anonymous) | pass (principal set) | 401 |

A present-but-invalid credential is rejected even on optional routes, so a
broken client surfaces rather than silently degrading to anonymous.

## Reading the principal

```go
func me(c *router.Context) {
    user, ok := auth.Principal[string](c.Ctx())   // sole scheme
    // or: auth.PrincipalFor[Claims](c.Ctx(), "bearerAuth")
    _ = ok
    c.JSON(http.StatusOK, map[string]string{"user": user})
}
```

`Principal[T]` returns the single resolved principal (the common one-scheme
case); `PrincipalFor[T]` selects by scheme name when a route requires several
schemes together via `SecurityAll`.

## How it works

The middleware calls `router.Match` to find the endpoint, reads its declared
requirements with `openapi.SecurityOf`, and evaluates the OR-list: an empty
requirement (`{}`, from `OptionalSecurity`) allows anonymous, and every other
alternative runs its schemes' verifiers (AND-combined). Routes with no security
annotation, or `NoSecurity`, pass straight through. Because enforcement reads
the annotation the spec is generated from, the documented contract and the
runtime gate are always the same.

A runnable end-to-end demo lives in `examples/auth/`.
