# Scalar UI

The `scalar` module serves the [Scalar API Reference](https://scalar.com)
as an `http.HandlerFunc`, configured to render the OpenAPI document
emitted by the `openapi` package.

Scalar is a modern, JavaScript-rendered reference UI that supports the
OpenAPI 3.x family — including the OAS 3.2 fields that minmux generates
(`deviceAuthorization`, `oauth2MetadataUrl`, streaming `itemSchema` /
`itemEncoding`, etc.).

## Install

```bash
go get github.com/joakimcarlsson/minmux/scalar
```

Zero dependencies beyond the standard library.

## Minimal use

Serve the spec next to a `/docs` route that points at it:

```go
import (
    "net/http"

    "github.com/joakimcarlsson/minmux/openapi"
    "github.com/joakimcarlsson/minmux/router"
    "github.com/joakimcarlsson/minmux/scalar"
)

func main() {
    r := router.New()

    // ... register routes with openapi annotations ...

    gen := openapi.NewGenerator(openapi.Info{
        Title:   "Pets API",
        Version: "0.1.0",
    })

    r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))
    r.HandleFunc(http.MethodGet, "/docs",         scalar.Handler("/openapi.json"))

    http.ListenAndServe(":8080", r)
}
```

Open `http://localhost:8080/docs` and Scalar fetches `/openapi.json` and
renders a browsable reference with a built-in API client.

## Configuration

`scalar.Handler(specURL)` is sugar for `scalar.HandlerWith(scalar.Config{SpecURL: specURL})`.
Use `HandlerWith` when you need more than the default:

```go
r.HandleFunc(http.MethodGet, "/docs", scalar.HandlerWith(scalar.Config{
    SpecURL:  "/openapi.json",
    Title:    "Pets API — Reference",
    Theme:    "moon",
    ProxyURL: "https://proxy.scalar.com", // only if spec is cross-origin
    CDNURL:   "",                         // override to pin a version or self-host
}))
```

| Field | Purpose |
|---|---|
| `SpecURL` | URL the UI fetches the OpenAPI document from. Same-origin paths like `/openapi.json` need no proxy. |
| `Title` | Page `<title>`. Defaults to `"API Reference"`. |
| `Theme` | Scalar theme name (e.g. `default`, `moon`, `purple`). Empty uses Scalar's default. |
| `ProxyURL` | Optional CORS proxy passed to Scalar as `proxyUrl`. Set only when `SpecURL` is cross-origin and the server lacks CORS. |
| `CDNURL` | Override the `<script src>`. Empty uses `scalar.DefaultCDNURL` (jsDelivr, unpinned `@latest`). Set to a self-hosted bundle for airgapped deployments, or pin a major like `@scalar/api-reference@1` for stability. |
| `Authentication` | Pre-fills Scalar's Authorize dialog (default client_id, scopes, preferred scheme). See [Pre-filling the Authorize dialog](#pre-filling-the-authorize-dialog). |

## Pre-filling the Authorize dialog

Scalar shows an Authorize button next to every secured operation; clicking
it opens a dialog where the user types their client_id (and, for some
flows, scopes / secret) before running an OAuth2 flow. For demo or
internal-staging instances you can pre-fill those values via
`Config.Authentication`:

```go
r.HandleFunc(http.MethodGet, "/docs", scalar.HandlerWith(scalar.Config{
    SpecURL: "/openapi.json",
    Authentication: &scalar.Authentication{
        PreferredSecurityScheme: "oauth2",
        SecuritySchemes: map[string]scalar.SchemeAuth{
            "oauth2": {
                Flows: map[string]scalar.FlowAuth{
                    "authorizationCode": {
                        ClientID:       "spa-app",
                        SelectedScopes: []string{"profile:read", "todos:write"},
                    },
                    "clientCredentials": {
                        ClientID:       "svc-reporter",
                        ClientSecret:   "s3cret", // demo only — never a real production secret
                        SelectedScopes: []string{"metrics:read"},
                    },
                    "deviceAuthorization": {
                        ClientID:       "tv-app",
                        SelectedScopes: []string{"channels:read"},
                    },
                },
            },
        },
    },
}))
```

Field reference:

| Field | Purpose |
|---|---|
| `Authentication.PreferredSecurityScheme` | Scheme name (as it appears in `components.securitySchemes`) that the Authorize dialog opens to when multiple schemes exist. |
| `SchemeAuth.Flows` | Per-flow OAuth2 prefill, keyed by OAS flow name: `authorizationCode`, `clientCredentials`, `deviceAuthorization`, `password`, `implicit`. |
| `SchemeAuth.Token` | Prefilled bearer / API-key value (for `http` and `apiKey` schemes, not `oauth2`). |
| `FlowAuth.ClientID` | Serialised as Scalar's `x-scalar-client-id` extension key. |
| `FlowAuth.ClientSecret` | Prefills the secret field for confidential-client flows. |
| `FlowAuth.SelectedScopes` | Scopes that start checked in the dialog. |
| `FlowAuth.Token` | Prefills an access token directly, skipping the live flow. |

This is a Scalar-side convenience: the values live in the rendered HTML,
not in the OpenAPI document, so other UIs (Redoc, Swagger UI) won't see
them. Never commit a real production secret here.

`examples/oauth2/` uses this to pre-fill demo client IDs for all three
OAuth2 flows it hosts, so the Authorize dialog opens with the right
values already filled in.

## Configuring base URLs

Scalar's base-URL selector (and pre-filled "Try it" requests) is driven by
the standard OpenAPI `servers` array, which the `openapi` package emits
from `Generator.Servers`:

```go
gen.Servers = []*openapi.Server{
    {URL: "http://localhost:8080",         Description: "This local instance"},
    {URL: "https://api.example.com/v1",    Description: "Production"},
    {
        URL:         "https://{environment}.example.com/v1",
        Description: "Non-production tiers",
        Variables: map[string]*openapi.ServerVariable{
            "environment": {
                Default:     "staging",
                Enum:        []string{"staging", "dev"},
                Description: "Deployment tier",
            },
        },
    },
}
```

This is a property of the OpenAPI document, not of the Scalar handler, so
it works the same for any UI you point at the spec.

## What the handler returns

A single static HTML page (computed once at handler construction):

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>API Reference</title>
  <style>body { margin: 0 }</style>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  <script>
    Scalar.createApiReference('#app', {"url":"/openapi.json"});
  </script>
</body>
</html>
```

Response headers: `Content-Type: text/html; charset=utf-8`, `Cache-Control: no-cache`.

The Scalar config is JSON-encoded with `encoding/json`, which escapes
`<`, `>`, and `&` to their Unicode forms — so user-supplied values
(titles, URLs) can't break out of the inline `<script>`.

## Runnable example

See [`examples/scalar-ui/`](https://github.com/joakimcarlsson/minmux/tree/main/examples/scalar-ui)
for a small Pets API wired up end-to-end. Run it:

```bash
cd examples/scalar-ui
go run .
# spec at http://localhost:8080/openapi.json
# docs at http://localhost:8080/docs
```
