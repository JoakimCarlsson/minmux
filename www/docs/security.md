# Security

minmux supports the full OpenAPI 3.2.0 security model: API keys, HTTP
auth (Basic / Bearer), mutual TLS, OAuth2 (including the new
`deviceAuthorization` flow), and OpenID Connect. Authentication is
declarative — you register schemes on the `Generator`, set a
document-level default, and override per group or per endpoint with
options that compose like the rest of the openapi package.

The runtime router does **not** enforce auth. The generated spec
describes what consumers must satisfy; enforcement is conventional
middleware (your JWT validator, mTLS terminator, etc.). This keeps the
router dependency-free and lets you slot in any auth stack.

A runnable end-to-end showcase lives in `examples/security/`. For a
self-contained authorization server that actually mints tokens through
the Authorization Code + PKCE, Client Credentials, and Device
Authorization flows — and a Scalar UI wired to use them — see
`examples/oauth2/`.

## Registering schemes

`Generator.SecuritySchemes` is a `map[string]*SecurityScheme` that ends
up under `components.securitySchemes`. The package ships ergonomic
constructors so you don't have to remember the OAS string literals:

```go
gen := openapi.NewGenerator(openapi.Info{Title: "API", Version: "0.1.0"})

gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
    "bearerAuth": openapi.BearerAuth("JWT", "Bearer JWT from the IdP"),
    "apiKeyAuth": openapi.APIKey("header", "X-Api-Key", ""),
    "basicAuth":  openapi.BasicAuth(""),
    "mtls":       openapi.MutualTLS("Client cert must chain to example.com CA"),
    "oidc":       openapi.OpenIDConnect(
        "https://issuer.example/.well-known/openid-configuration", "",
    ),
}
```

| Constructor | OAS Type | Notes |
|---|---|---|
| `BasicAuth(desc)` | `http` `scheme: basic` | RFC 7617 |
| `BearerAuth(format, desc)` | `http` `scheme: bearer` | `bearerFormat` is a hint (e.g. `"JWT"`); pass `""` to omit |
| `APIKey(in, name, desc)` | `apiKey` | `in` is `"header"`, `"query"`, or `"cookie"` |
| `MutualTLS(desc)` | `mutualTLS` | OAS 3.1+; client-cert auth |
| `OpenIDConnect(url, desc)` | `openIdConnect` | Discovery URL is required |
| `OAuth2Scheme(flows, desc)` | `oauth2` | See below |

Each constructor returns a `*SecurityScheme` you can keep mutating —
useful when a scheme needs a 3.2-only field like `OAuth2MetadataURL`:

```go
oauth := openapi.OAuth2Scheme(flows, "")
oauth.OAuth2MetadataURL = "https://auth.example/.well-known/oauth-authorization-server"
oauth.Deprecated = true
```

## OAuth2 and the device authorization flow

`OAuthFlows` mirrors the OAS structure 1:1. You build it directly so
each flow only carries the URL fields it actually requires (the spec is
strict about which URL applies to which flow). OAS 3.2 introduces
`deviceAuthorization` (RFC 8628), which is selected by setting
`DeviceAuthorizationURL` alongside `TokenURL`:

```go
gen.SecuritySchemes["petstoreOAuth"] = openapi.OAuth2Scheme(
    &openapi.OAuthFlows{
        AuthorizationCode: &openapi.OAuthFlow{
            AuthorizationURL: "https://auth.example/oauth/authorize",
            TokenURL:         "https://auth.example/oauth/token",
            RefreshURL:       "https://auth.example/oauth/refresh",
            Scopes: map[string]string{
                "read:pets":  "List and read pets",
                "write:pets": "Create, update, and delete pets",
            },
        },
        ClientCredentials: &openapi.OAuthFlow{
            TokenURL: "https://auth.example/oauth/token",
            Scopes:   map[string]string{"admin:pets": "Admin"},
        },
        DeviceAuthorization: &openapi.OAuthFlow{
            DeviceAuthorizationURL: "https://auth.example/oauth/device_authorization",
            TokenURL:               "https://auth.example/oauth/token",
            Scopes: map[string]string{
                "read:pets": "List and read pets (from a TV or CLI)",
            },
        },
    },
    "Pet store user OAuth2 grants",
)
```

The `Scopes` map is always emitted — even when empty — because OAS marks
it `REQUIRED`. Implicit and Password flows are also supported but the
OAuth 2.0 Security BCP recommends Authorization Code + PKCE or Device
Authorization for new APIs.

## Document-level default

`Generator.Security` is the root `security` array. Every operation that
doesn't carry its own `security` inherits this list:

```go
gen.Security = []openapi.SecurityRequirement{
    {"bearerAuth": {}},
}
```

A `SecurityRequirement` is a `map[string][]string` where the key is a
scheme name and the value is the required scopes (oauth2 / oidc) or
role hints (everything else). Multiple `SecurityRequirement` entries in
the array are **OR-combined**: any one is enough.

## Per-endpoint options

Four options shape the per-operation `security` array. Group options
cascade to every endpoint registered through the group, exactly like
`Tags`.

| Option | Effect |
|---|---|
| `Security(scheme, scopes...)` | Append one alternative requiring `scheme` with the given scopes. Multiple calls = OR alternatives. |
| `SecurityAll(req SecurityRequirement)` | Append one alternative that requires **all** keys in `req` together. |
| `OptionalSecurity()` | Append the empty `{}` requirement; allows anonymous as one of the alternatives. |
| `NoSecurity()` | Emit `"security": []` on the operation. Clears any document- or group-level default and discards previously-added options on the same endpoint. |

A representative spread:

```go
r.Get("/health", health,
    openapi.NoSecurity(), // never requires auth, even if doc-level default is set
)

r.Get("/me", me,
    // (no security option) — inherits doc-level bearerAuth default
)

pets := r.Group("/pets",
    openapi.Tags("Pets"),
    openapi.Security("petstoreOAuth", "read:pets"),
)

pets.Get("", listPets,
    openapi.OptionalSecurity(), // OR: read:pets, OR: anonymous
)

pets.Post("", createPet,
    // OR: read:pets (group), OR: write:pets (this option)
    openapi.Security("petstoreOAuth", "write:pets"),
)

pets.Delete("/{id}", deletePet,
    // OR: read:pets (group), OR: mTLS AND admin:pets simultaneously
    openapi.SecurityAll(openapi.SecurityRequirement{
        "mtls":          {},
        "petstoreOAuth": {"admin:pets"},
    }),
)
```

`SecurityAll` deep-copies its input, so mutating the map after the
option call doesn't leak into the spec.

## Inherit vs explicit-empty

The generator distinguishes three states on an operation:

| Endpoint declares | Emitted JSON | Meaning |
|---|---|---|
| nothing | field omitted | inherit document-level `security` |
| any `Security` / `SecurityAll` / `OptionalSecurity` | `"security": [ ... ]` | operation-specific requirements (overrides doc default) |
| `NoSecurity()` | `"security": []` | explicitly no auth (overrides doc default with empty array) |

Per OAS 3.2 §4.30, an empty array on an operation removes the
top-level default, while an empty requirement object (`{}`) inside the
array means "anonymous is one acceptable alternative".

## Spec output

`examples/security/` registers all six scheme types and the spread of
endpoint options above. Hitting `/openapi.json` produces, among other
things:

```json
{
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      },
      "petstoreOAuth": {
        "type": "oauth2",
        "flows": {
          "authorizationCode": {
            "authorizationUrl": "https://auth.example/oauth/authorize",
            "tokenUrl": "https://auth.example/oauth/token",
            "refreshUrl": "https://auth.example/oauth/refresh",
            "scopes": {
              "read:pets": "List and read pets",
              "write:pets": "Create, update, and delete pets"
            }
          },
          "deviceAuthorization": {
            "deviceAuthorizationUrl": "https://auth.example/oauth/device_authorization",
            "tokenUrl": "https://auth.example/oauth/token",
            "scopes": { "read:pets": "List and read pets (from a TV or CLI)" }
          }
        },
        "oauth2MetadataUrl": "https://auth.example/.well-known/oauth-authorization-server"
      }
    }
  },
  "security": [
    { "bearerAuth": [] }
  ],
  "paths": {
    "/health":       { "get":    { "security": [] } },
    "/pets":         { "get":    { "security": [ { "petstoreOAuth": ["read:pets"] }, {} ] } },
    "/pets/{id}":    { "delete": { "security": [
      { "petstoreOAuth": ["read:pets"] },
      { "mtls": [], "petstoreOAuth": ["admin:pets"] }
    ] } }
  }
}
```

To reproduce locally:

```bash
cd examples/security
go run . --check   # print spec to stdout, no listener
# or:
go run .           # listen on :8080
curl -s http://localhost:8080/openapi.json | jq
```

## Validator compatibility

The spec emitted here is conformant to OAS 3.2.0 §4.27–§4.30. Some
online validators are still catching up to 3.2 and will report false
positives on the OAuth2 fields introduced in this version:

- **`deviceAuthorization` flow** under `flows` and its
  `deviceAuthorizationUrl` field (§4.28.1, §4.29.1).
- **`oauth2MetadataUrl`** on the Security Scheme Object (§4.27.1).

At the time of writing, [editor.swagger.io](https://editor.swagger.io)
flags both as "Object includes not allowed fields" because its bundled
schema lags the 3.2 release. The spec validates cleanly against the
official OAS 3.2 JSON Schema published at
[spec.openapis.org](https://spec.openapis.org/oas/v3.2.0.html); use
tooling pinned to 3.2 (e.g. Redocly CLI's `--openapi-version 3.2.0`,
or a `vacuum lint` with the 3.2 ruleset) to validate without these
false positives.
