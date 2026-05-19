// oauth2 is a single-process showcase of three OAuth2 grant types
// served by one minmux router on :8080:
//
//   - Authorization Code + PKCE — /oauth/auth-code/{authorize,token}
//   - Client Credentials        — /oauth/client-credentials/token
//   - Device Authorization      — /oauth/device/{device_authorization,token}
//     plus a user-facing approval page at /device
//
// All three flows feed the same in-memory bearer store, and one OAS 3.2
// security scheme (`oauth2`) advertises all three flows under `flows:`
// so Scalar can render the full Authorize UI.
//
// Click-through:
//
//	go run ./examples/oauth2
//	open http://localhost:8080/docs
//
// Demo credentials baked in:
//
//	auth-code        client_id=spa-app        (PKCE S256 required)
//	client-creds     client_id=svc-reporter   secret=s3cret
//	device           client_id=tv-app
//
// In-memory state, opaque tokens, short TTLs — teaching toy only.
package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
	"github.com/joakimcarlsson/minmux/scalar"
)

const (
	addr      = ":8080"
	issuer    = "http://localhost:8080"
	accessTTL = 10 * time.Minute
	codeTTL   = 60 * time.Second
	deviceTTL = 5 * time.Minute
	pollMin   = 2 * time.Second
)

// store is the shared in-memory state for all three flows.
type store struct {
	mu sync.Mutex

	// auth-code+pkce
	codes map[string]authCode

	// device
	byDeviceCode map[string]*deviceGrant
	byUserCode   map[string]*deviceGrant

	// bearer tokens issued by any flow
	tokens map[string]accessToken
}

func newStore() *store {
	return &store{
		codes:        map[string]authCode{},
		byDeviceCode: map[string]*deviceGrant{},
		byUserCode:   map[string]*deviceGrant{},
		tokens:       map[string]accessToken{},
	}
}

func main() {
	s := newStore()
	r := router.New()
	r.Use(router.Recover())

	oauthSrv := r.Group("", openapi.Tags("OAuth2 Server"), openapi.NoSecurity())

	oauthSrv.Get(
		"/oauth/auth-code/authorize",
		s.authCodeAuthorizeGET,
		openapi.Summary("Authorization endpoint (consent screen)"),
		openapi.Description(
			"Renders an HTML consent page. PKCE S256 required.",
		),
		openapi.Returns(http.StatusOK, "Consent HTML"),
	)
	oauthSrv.Post(
		"/oauth/auth-code/authorize",
		s.authCodeAuthorizePOST,
		openapi.Summary("Authorization endpoint (decision submit)"),
		openapi.Description(
			"Redirects back to redirect_uri with ?code= on allow, ?error= on deny.",
		),
		openapi.Returns(http.StatusFound, "Redirect to redirect_uri"),
	)
	oauthSrv.Post("/oauth/auth-code/token", s.authCodeToken,
		openapi.Summary("Token endpoint (authorization_code)"),
		openapi.ReturnsBody[TokenResponse](http.StatusOK, "Access token"),
		openapi.ReturnsBody[TokenError](http.StatusBadRequest, "OAuth2 error"),
	)
	oauthSrv.Post(
		"/oauth/client-credentials/token",
		s.clientCredsToken,
		openapi.Summary("Token endpoint (client_credentials)"),
		openapi.Description(
			"Credentials may be HTTP Basic or in the form body.",
		),
		openapi.ReturnsBody[TokenResponse](http.StatusOK, "Access token"),
		openapi.ReturnsBody[TokenError](http.StatusBadRequest, "OAuth2 error"),
	)
	oauthSrv.Post(
		"/oauth/device/device_authorization",
		s.deviceAuth,
		openapi.Summary("Device authorization endpoint (RFC 8628 §3.1)"),
		openapi.ReturnsBody[DeviceCodeResponse](
			http.StatusOK,
			"device_code + user_code",
		),
		openapi.ReturnsBody[TokenError](http.StatusBadRequest, "OAuth2 error"),
	)
	oauthSrv.Post(
		"/oauth/device/token",
		s.deviceToken,
		openapi.Summary("Token endpoint (device_code)"),
		openapi.Description(
			"Poll until the user approves. Returns authorization_pending / slow_down / access_denied / expired_token until then.",
		),
		openapi.ReturnsBody[TokenResponse](http.StatusOK, "Access token"),
		openapi.ReturnsBody[TokenError](
			http.StatusBadRequest,
			"Pending or error",
		),
	)
	oauthSrv.Get(
		"/device",
		s.deviceVerifyGET,
		openapi.Summary("Device verification page"),
		openapi.Description(
			"User-facing HTML form: paste the user_code shown on the device, then approve or deny.",
		),
		openapi.Returns(http.StatusOK, "Verification HTML"),
	)
	oauthSrv.Post("/device", s.deviceVerifyPOST,
		openapi.Summary("Device verification decision"),
		openapi.Returns(http.StatusOK, "Result HTML"),
	)

	api := r.Group("/api", router.Middleware(s.requireToken))

	api.Get(
		"/profile",
		profile,
		openapi.Tags("Auth Code"),
		openapi.Summary("Current principal (auth-code+pkce)"),
		openapi.Security("oauth2", "profile:read"),
		openapi.ReturnsBody[Profile](http.StatusOK, "Profile"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusUnauthorized,
			"Missing or invalid token",
		),
	)
	api.Post(
		"/todos",
		createTodo,
		openapi.Tags("Auth Code"),
		openapi.Summary("Create a todo"),
		openapi.Security("oauth2", "todos:write"),
		openapi.ReturnsBody[TodoItem](http.StatusCreated, "Created"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusForbidden,
			"Missing scope",
		),
	)

	api.Get("/metrics", listMetrics,
		openapi.Tags("Client Credentials"),
		openapi.Summary("List metrics"),
		openapi.Security("oauth2", "metrics:read"),
		openapi.ReturnsBody[[]Metric](http.StatusOK, "Metrics"),
	)
	api.Post("/metrics", writeMetric,
		openapi.Tags("Client Credentials"),
		openapi.Summary("Push a metric"),
		openapi.Security("oauth2", "metrics:write"),
		openapi.ReturnsBody[Metric](http.StatusCreated, "Accepted"),
	)

	api.Get("/channels", listChannels,
		openapi.Tags("Device"),
		openapi.Summary("List channels"),
		openapi.Security("oauth2", "channels:read"),
		openapi.ReturnsBody[[]Channel](http.StatusOK, "Channels"),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:   "minmux OAuth2 Flows",
		Version: "0.1.0",
		Description: "Self-contained AS + RS exercising the three commonly " +
			"used OAuth2 flows (Authorization Code + PKCE, Client Credentials, " +
			"Device Authorization). One OAuth2 securityScheme with all three " +
			"flows declared under `flows:`.",
	})
	gen.Servers = []*openapi.Server{{URL: issuer, Description: "Local"}}
	gen.Tags = []*openapi.Tag{
		{Name: "Auth Code", Description: "Authorization Code + PKCE flow"},
		{
			Name:        "Client Credentials",
			Description: "Service-to-service OAuth2 grant",
		},
		{Name: "Device", Description: "RFC 8628 device authorization grant"},
	}

	// One scheme, three flows — the canonical OAS 3.x way of saying
	// "this API accepts a token from any of these grant types."
	gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
		"oauth2": openapi.OAuth2Scheme(&openapi.OAuthFlows{
			AuthorizationCode: &openapi.OAuthFlow{
				AuthorizationURL: issuer + "/oauth/auth-code/authorize",
				TokenURL:         issuer + "/oauth/auth-code/token",
				Scopes: map[string]string{
					"profile:read": "Read your profile",
					"todos:write":  "Create todos",
				},
			},
			ClientCredentials: &openapi.OAuthFlow{
				TokenURL: issuer + "/oauth/client-credentials/token",
				Scopes: map[string]string{
					"metrics:read":  "Read metrics",
					"metrics:write": "Push metrics",
				},
			},
			DeviceAuthorization: &openapi.OAuthFlow{
				DeviceAuthorizationURL: issuer + "/oauth/device/device_authorization",
				TokenURL:               issuer + "/oauth/device/token",
				Scopes: map[string]string{
					"channels:read": "List channels",
				},
			},
		}, "All OAuth2 flows hosted by this server"),
	}

	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))
	r.HandleFunc(http.MethodGet, "/docs", scalar.HandlerWith(scalar.Config{
		SpecURL: "/openapi.json",
		Title:   "minmux OAuth2 Flows",
		// Pre-fill the Authorize dialog with the baked-in demo client IDs
		// (and the client_credentials secret) so visitors can click Try-It
		// without copying values out of the source.
		Authentication: &scalar.Authentication{
			PreferredSecurityScheme: "oauth2",
			SecuritySchemes: map[string]scalar.SchemeAuth{
				"oauth2": {
					Flows: map[string]scalar.FlowAuth{
						"authorizationCode": {
							ClientID: authCodeClientID,
							SelectedScopes: []string{
								"profile:read",
								"todos:write",
							},
						},
						"clientCredentials": {
							ClientID:     "svc-reporter",
							ClientSecret: "s3cret",
							SelectedScopes: []string{
								"metrics:read",
								"metrics:write",
							},
						},
						"deviceAuthorization": {
							ClientID:       deviceClientID,
							SelectedScopes: []string{"channels:read"},
						},
					},
				},
			},
		},
	}))

	fmt.Println(
		"listening on",
		addr,
		"(docs at /docs, device verify at /device)",
	)
	log.Fatal(http.ListenAndServe(addr, r))
}
