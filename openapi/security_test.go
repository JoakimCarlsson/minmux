package openapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

func TestSecurityScheme_BasicAuth(t *testing.T) {
	s := BasicAuth("admin only")
	if s.Type != "http" || s.Scheme != "basic" {
		t.Fatalf("basic: %+v", s)
	}
	if s.Description != "admin only" {
		t.Errorf("description: %q", s.Description)
	}
	got := marshalField(t, s)
	if got["type"] != "http" || got["scheme"] != "basic" {
		t.Errorf("basic json: %v", got)
	}
	if _, ok := got["name"]; ok {
		t.Errorf("basic should omit name, got %v", got)
	}
}

func TestSecurityScheme_BearerJWT(t *testing.T) {
	s := BearerAuth("JWT", "")
	got := marshalField(t, s)
	if got["type"] != "http" || got["scheme"] != "bearer" {
		t.Errorf("bearer type/scheme: %v", got)
	}
	if got["bearerFormat"] != "JWT" {
		t.Errorf("bearerFormat: %v", got)
	}
}

func TestSecurityScheme_APIKey(t *testing.T) {
	s := APIKey("header", "X-API-Key", "")
	got := marshalField(t, s)
	if got["type"] != "apiKey" || got["in"] != "header" ||
		got["name"] != "X-API-Key" {
		t.Errorf("apikey: %v", got)
	}
}

func TestSecurityScheme_MutualTLS(t *testing.T) {
	s := MutualTLS("cert signed by example.com")
	got := marshalField(t, s)
	if got["type"] != "mutualTLS" {
		t.Errorf("mutualTLS type: %v", got)
	}
	if got["description"] != "cert signed by example.com" {
		t.Errorf("mutualTLS description: %v", got)
	}
}

func TestSecurityScheme_OpenIDConnect(t *testing.T) {
	s := OpenIDConnect(
		"https://issuer.example/.well-known/openid-configuration",
		"",
	)
	got := marshalField(t, s)
	if got["type"] != "openIdConnect" {
		t.Errorf("oidc type: %v", got)
	}
	if got["openIdConnectUrl"] !=
		"https://issuer.example/.well-known/openid-configuration" {
		t.Errorf("oidc url: %v", got)
	}
}

func TestSecurityScheme_OAuth2_AllFlows(t *testing.T) {
	scheme := OAuth2Scheme(&OAuthFlows{
		Implicit: &OAuthFlow{
			AuthorizationURL: "https://ex/auth",
			Scopes:           map[string]string{"read": "read"},
		},
		Password: &OAuthFlow{
			TokenURL: "https://ex/token",
			Scopes:   map[string]string{},
		},
		ClientCredentials: &OAuthFlow{
			TokenURL: "https://ex/token",
			Scopes:   map[string]string{},
		},
		AuthorizationCode: &OAuthFlow{
			AuthorizationURL: "https://ex/auth",
			TokenURL:         "https://ex/token",
			RefreshURL:       "https://ex/refresh",
			Scopes:           map[string]string{"write": "write"},
		},
		DeviceAuthorization: &OAuthFlow{
			DeviceAuthorizationURL: "https://ex/device",
			TokenURL:               "https://ex/token",
			Scopes:                 map[string]string{"read": "read"},
		},
	}, "")
	scheme.OAuth2MetadataURL = "https://ex/.well-known/oauth-authorization-server"

	raw, err := json.Marshal(scheme)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := map[string]any{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "oauth2" {
		t.Errorf("type: %v", got["type"])
	}
	if got["oauth2MetadataUrl"] !=
		"https://ex/.well-known/oauth-authorization-server" {
		t.Errorf("oauth2MetadataUrl: %v", got["oauth2MetadataUrl"])
	}
	flows, ok := got["flows"].(map[string]any)
	if !ok {
		t.Fatalf("flows missing: %v", got)
	}
	for _, name := range []string{
		"implicit", "password", "clientCredentials",
		"authorizationCode", "deviceAuthorization",
	} {
		if _, ok := flows[name]; !ok {
			t.Errorf("flow %q missing: %v", name, flows)
		}
	}
	device := flows["deviceAuthorization"].(map[string]any)
	if device["deviceAuthorizationUrl"] != "https://ex/device" {
		t.Errorf("device url: %v", device)
	}
	if device["tokenUrl"] != "https://ex/token" {
		t.Errorf("device tokenUrl: %v", device)
	}
}

func TestOAuthFlow_ScopesAlwaysEmitted(t *testing.T) {
	f := &OAuthFlow{
		TokenURL: "https://ex/token",
		Scopes:   map[string]string{},
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"scopes":{}`) {
		t.Errorf("scopes must be emitted even when empty: %s", raw)
	}
}

func TestSpec_SecuritySchemesEmittedInComponents(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)

	g := NewGenerator(Info{Title: "T", Version: "1"})
	g.SecuritySchemes = map[string]*SecurityScheme{
		"bearerAuth": BearerAuth("JWT", ""),
		"apiKey":     APIKey("header", "X-Api-Key", ""),
	}

	spec := g.Spec(r)
	if spec.Components == nil {
		t.Fatal(
			"components should be created when only security schemes are set",
		)
	}
	if len(spec.Components.SecuritySchemes) != 2 {
		t.Errorf("schemes: %v", spec.Components.SecuritySchemes)
	}
	if spec.Components.Schemas != nil {
		t.Errorf("schemas should be nil, got %v", spec.Components.Schemas)
	}
}

func TestSpec_DocumentLevelSecurityEmitted(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)

	g := NewGenerator(Info{Title: "T", Version: "1"})
	g.SecuritySchemes = map[string]*SecurityScheme{
		"bearerAuth": BearerAuth("JWT", ""),
	}
	g.Security = []SecurityRequirement{
		{"bearerAuth": {}},
	}

	raw, err := json.Marshal(g.Spec(r))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"security":[{"bearerAuth":[]}]`) {
		t.Errorf("root security missing: %s", raw)
	}
}

func TestOperation_SecuritySingleAlternative(t *testing.T) {
	r := router.New()
	r.Get("/u", noop, Security("bearerAuth", "read:user"))

	op := operation(t, r, "/u", "GET")
	if op.Security == nil {
		t.Fatal("security nil")
	}
	want := []SecurityRequirement{
		{"bearerAuth": {"read:user"}},
	}
	if !reflect.DeepEqual(*op.Security, want) {
		t.Errorf("security: got %v want %v", *op.Security, want)
	}
}

func TestOperation_SecurityMultipleAlternatives(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		Security("bearerAuth"),
		Security("apiKey"),
	)
	op := operation(t, r, "/u", "GET")
	if op.Security == nil || len(*op.Security) != 2 {
		t.Fatalf("alternatives: %+v", op.Security)
	}
	if _, ok := (*op.Security)[0]["bearerAuth"]; !ok {
		t.Errorf("first alt: %v", (*op.Security)[0])
	}
	if _, ok := (*op.Security)[1]["apiKey"]; !ok {
		t.Errorf("second alt: %v", (*op.Security)[1])
	}
}

func TestOperation_SecurityAllAnded(t *testing.T) {
	r := router.New()
	r.Get("/u", noop, SecurityAll(SecurityRequirement{
		"apiKey":    {},
		"signature": {},
	}))
	op := operation(t, r, "/u", "GET")
	if op.Security == nil || len(*op.Security) != 1 {
		t.Fatalf("want one compound requirement, got %+v", op.Security)
	}
	req := (*op.Security)[0]
	if _, ok := req["apiKey"]; !ok {
		t.Errorf("apiKey missing: %v", req)
	}
	if _, ok := req["signature"]; !ok {
		t.Errorf("signature missing: %v", req)
	}
}

func TestOperation_SecurityAllCopiesInput(t *testing.T) {
	input := SecurityRequirement{"apiKey": {"a"}}
	r := router.New()
	r.Get("/u", noop, SecurityAll(input))

	input["apiKey"][0] = "MUTATED"
	input["extra"] = []string{"x"}

	op := operation(t, r, "/u", "GET")
	req := (*op.Security)[0]
	if req["apiKey"][0] != "a" {
		t.Errorf("mutation leaked into option: %v", req)
	}
	if _, ok := req["extra"]; ok {
		t.Errorf("post-call key leaked: %v", req)
	}
}

func TestOperation_OptionalSecurity(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		Security("bearerAuth"),
		OptionalSecurity(),
	)
	op := operation(t, r, "/u", "GET")
	if op.Security == nil || len(*op.Security) != 2 {
		t.Fatalf("want 2 entries, got %+v", op.Security)
	}
	if len((*op.Security)[1]) != 0 {
		t.Errorf("second entry should be empty {}, got %v", (*op.Security)[1])
	}
}

func TestOperation_NoSecurityEmitsEmptyArray(t *testing.T) {
	r := router.New()
	r.Get("/u", noop, NoSecurity())

	spec := NewGenerator(Info{Title: "T", Version: "1"}).Spec(r)
	raw, err := json.Marshal(spec.Paths["/u"].Get)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"security":[]`) {
		t.Errorf("want explicit empty array, got %s", raw)
	}
}

func TestOperation_NoSecurityOverridesPreviousSecurity(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		Security("bearerAuth"),
		NoSecurity(),
	)
	op := operation(t, r, "/u", "GET")
	if op.Security == nil {
		t.Fatal("security pointer must be non-nil to emit []")
	}
	if len(*op.Security) != 0 {
		t.Errorf("want empty, got %+v", *op.Security)
	}
}

func TestOperation_NoSecurityInJSON(t *testing.T) {
	r := router.New()
	r.Get("/with", noop, Security("bearerAuth"))
	r.Get("/without", noop, NoSecurity())
	r.Get("/inherit", noop)

	g := NewGenerator(Info{Title: "T", Version: "1"})
	g.SecuritySchemes = map[string]*SecurityScheme{
		"bearerAuth": BearerAuth("JWT", ""),
	}
	g.Security = []SecurityRequirement{{"bearerAuth": {}}}

	raw, err := json.Marshal(g.Spec(r))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"/inherit":{"get":{"responses"`) {
		t.Errorf(
			"/inherit must omit security to inherit doc-level default: %s",
			s,
		)
	}
	if !strings.Contains(
		s,
		`"/with":{"get":{"responses":{"default":{"description":"Default response"}},"security":[{"bearerAuth":[]}]}}`,
	) {
		t.Errorf("/with should carry single bearerAuth requirement: %s", s)
	}
	if !strings.Contains(
		s,
		`"/without":{"get":{"responses":{"default":{"description":"Default response"}},"security":[]}}`,
	) {
		t.Errorf("/without should carry explicit empty security: %s", s)
	}
}

func TestOperation_InheritsDocumentDefaultWhenUnset(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)
	op := operation(t, r, "/u", "GET")
	if op.Security != nil {
		t.Errorf(
			"operation should leave security nil to inherit, got %+v",
			*op.Security,
		)
	}
}

func TestGroup_SecurityCascadesToEndpoints(t *testing.T) {
	r := router.New()
	api := r.Group("/api", Security("bearerAuth", "read"))
	api.Get("/items", noop)
	api.Get("/items/{x}", noop, Security("apiKey"))
	api.Get("/public", noop, NoSecurity())

	itemsOp := operation(t, r, "/api/items", "GET")
	if itemsOp.Security == nil || len(*itemsOp.Security) != 1 {
		t.Fatalf("/api/items: %+v", itemsOp.Security)
	}
	if scopes, ok := (*itemsOp.Security)[0]["bearerAuth"]; !ok ||
		!reflect.DeepEqual(scopes, []string{"read"}) {
		t.Errorf("/api/items bearerAuth scopes: %v", (*itemsOp.Security)[0])
	}

	itemOp := operation(t, r, "/api/items/{x}", "GET")
	if itemOp.Security == nil || len(*itemOp.Security) != 2 {
		t.Fatalf("/api/items/{x}: %+v", itemOp.Security)
	}
	if _, ok := (*itemOp.Security)[0]["bearerAuth"]; !ok {
		t.Errorf("group bearerAuth missing: %v", (*itemOp.Security)[0])
	}
	if _, ok := (*itemOp.Security)[1]["apiKey"]; !ok {
		t.Errorf("endpoint apiKey missing: %v", (*itemOp.Security)[1])
	}

	publicOp := operation(t, r, "/api/public", "GET")
	if publicOp.Security == nil || len(*publicOp.Security) != 0 {
		t.Errorf(
			"NoSecurity should clear cascaded group security: %+v",
			publicOp.Security,
		)
	}
}

func TestSpec_JSONFieldOrder_WithSecurity(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)

	g := NewGenerator(Info{Title: "T", Version: "1"})
	g.SecuritySchemes = map[string]*SecurityScheme{
		"bearerAuth": BearerAuth("JWT", ""),
	}
	g.Security = []SecurityRequirement{{"bearerAuth": {}}}

	raw, err := json.Marshal(g.Spec(r))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	idxPaths := strings.Index(s, `"paths"`)
	idxComponents := strings.Index(s, `"components"`)
	idxSecurity := strings.Index(s, `"security"`)
	if idxPaths == -1 || idxComponents == -1 || idxSecurity == -1 {
		t.Fatalf("missing field: %s", s)
	}
	if !(idxPaths < idxComponents && idxComponents < idxSecurity) {
		t.Errorf("field order wrong: paths=%d components=%d security=%d in %s",
			idxPaths, idxComponents, idxSecurity, s)
	}
}

// marshalField is a small helper that round-trips v through JSON into a
// generic map so tests can assert on the emitted shape rather than the Go
// struct shape.
func marshalField(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}
