package scalar

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

// DefaultCDNURL is the Scalar API Reference script served via jsDelivr.
// Override per-handler with Config.CDNURL to pin a version or self-host.
const DefaultCDNURL = "https://cdn.jsdelivr.net/npm/@scalar/api-reference"

// DefaultTitle is the <title> used when Config.Title is empty.
const DefaultTitle = "API Reference"

// Config configures a Scalar UI handler. Only SpecURL is required.
type Config struct {
	// SpecURL is the URL the UI fetches the OpenAPI document from.
	// Same-origin paths like "/openapi.json" work without a proxy.
	SpecURL string

	// Title sets the page <title>. Empty falls back to DefaultTitle.
	Title string

	// Theme is the Scalar theme name (e.g. "default", "moon", "purple").
	// Empty uses Scalar's built-in default.
	Theme string

	// ProxyURL is an optional CORS proxy passed to Scalar as proxyUrl.
	// Set this only when SpecURL is cross-origin and the server lacks CORS.
	ProxyURL string

	// CDNURL overrides the script src. Empty uses DefaultCDNURL.
	// Set to a self-hosted bundle for airgapped deployments.
	CDNURL string

	// Authentication pre-fills Scalar's Authorize dialog. Nil renders no
	// `authentication` key.
	Authentication *Authentication
}

// Authentication mirrors Scalar's createApiReference `authentication`
// option. Only the fields minmux's callers tend to need are typed;
// callers needing exotic shapes can vendor a copy.
type Authentication struct {
	// PreferredSecurityScheme is the scheme name highlighted in the
	// Authorize dialog when multiple schemes exist.
	PreferredSecurityScheme string `json:"preferredSecurityScheme,omitempty"`

	// SecuritySchemes carries per-scheme prefill, keyed by the scheme
	// name as it appears in the OpenAPI document's
	// `components.securitySchemes`.
	SecuritySchemes map[string]SchemeAuth `json:"securitySchemes,omitempty"`
}

// SchemeAuth is the prefill for a single named security scheme.
type SchemeAuth struct {
	// Flows carries per-flow OAuth2 prefill, keyed by the OAS flow name
	// (authorizationCode, clientCredentials, deviceAuthorization,
	// password, implicit).
	Flows map[string]FlowAuth `json:"flows,omitempty"`

	// Token is the prefilled bearer/apiKey value for http or apiKey
	// schemes (not used for oauth2).
	Token string `json:"token,omitempty"`
}

// FlowAuth is the prefill for one OAuth2 flow on one scheme.
type FlowAuth struct {
	// ClientID populates the Authorize dialog's client_id field.
	// Serialised as Scalar's `x-scalar-client-id` extension key.
	ClientID string `json:"x-scalar-client-id,omitempty"`

	// ClientSecret prefills the secret field for confidential client
	// flows (client_credentials, password). Only useful in demo
	// environments; never commit a real production secret here.
	ClientSecret string `json:"clientSecret,omitempty"`

	// SelectedScopes lists scopes that should start checked in the
	// Authorize dialog.
	SelectedScopes []string `json:"selectedScopes,omitempty"`

	// Token prefills the access token field, skipping the live flow.
	Token string `json:"token,omitempty"`
}

// Handler returns an http.HandlerFunc that serves the Scalar API Reference
// UI configured to load the OpenAPI document at specURL.
func Handler(specURL string) http.HandlerFunc {
	return HandlerWith(Config{SpecURL: specURL})
}

// HandlerWith is Handler with additional configuration.
func HandlerWith(cfg Config) http.HandlerFunc {
	title := cfg.Title
	if title == "" {
		title = DefaultTitle
	}
	cdn := cfg.CDNURL
	if cdn == "" {
		cdn = DefaultCDNURL
	}

	scalarCfg := map[string]any{"url": cfg.SpecURL}
	if cfg.Theme != "" {
		scalarCfg["theme"] = cfg.Theme
	}
	if cfg.ProxyURL != "" {
		scalarCfg["proxyUrl"] = cfg.ProxyURL
	}
	if cfg.Authentication != nil {
		scalarCfg["authentication"] = cfg.Authentication
	}
	// encoding/json escapes <, >, & as < > &, making the
	// payload safe to drop into an inline <script> regardless of caller
	// input.
	raw, err := json.Marshal(scalarCfg)
	if err != nil {
		raw = []byte("{}")
	}

	var buf strings.Builder
	_ = pageTpl.Execute(&buf, pageData{
		Title:      title,
		CDNURL:     cdn,
		ConfigJSON: template.JS(raw),
	})
	body := buf.String()

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body))
	}
}

type pageData struct {
	Title      string
	CDNURL     string
	ConfigJSON template.JS
}

var pageTpl = template.Must(template.New("scalar").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>body { margin: 0 }</style>
</head>
<body>
<div id="app"></div>
<script src="{{.CDNURL}}"></script>
<script>
Scalar.createApiReference('#app', {{.ConfigJSON}});
</script>
</body>
</html>
`))
