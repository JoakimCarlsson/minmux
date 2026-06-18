package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joakimcarlsson/minmux/auth"
	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

const scheme = "bearer"

func testVerifier(r *http.Request, _ []string) (any, error) {
	switch r.Header.Get("X-Token") {
	case "":
		return nil, auth.ErrNoCredential
	case "bad":
		return nil, errors.New("invalid token")
	case "forbidden":
		return nil, auth.ErrForbidden
	default:
		return "user-" + r.Header.Get("X-Token"), nil
	}
}

func newServer(t *testing.T, opts ...router.Option) *httptest.Server {
	t.Helper()
	r := router.New()
	authn := auth.New(r, auth.Config{
		Verifiers: map[string]auth.Verifier{scheme: testVerifier},
	})
	r.Use(authn.Middleware())
	r.Get("/x", func(c *router.Context) {
		who, _ := auth.Principal[string](c.Ctx())
		c.JSON(http.StatusOK, map[string]string{"who": who})
	}, opts...)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, ts *httptest.Server, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/x", nil)
	if token != "" {
		req.Header.Set("X-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Who string `json:"who"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body.Who
}

func TestEnforcement(t *testing.T) {
	cases := []struct {
		name     string
		opts     []router.Option
		token    string
		wantCode int
		wantWho  string
	}{
		{"no annotation passes", nil, "alice", http.StatusOK, ""},
		{"NoSecurity passes", []router.Option{openapi.NoSecurity()}, "alice", http.StatusOK, ""},
		{"required + valid", []router.Option{openapi.Security(scheme)}, "alice", http.StatusOK, "user-alice"},
		{"required + missing → 401", []router.Option{openapi.Security(scheme)}, "", http.StatusUnauthorized, ""},
		{"required + invalid → 401", []router.Option{openapi.Security(scheme)}, "bad", http.StatusUnauthorized, ""},
		{"required + forbidden → 403", []router.Option{openapi.Security(scheme)}, "forbidden", http.StatusForbidden, ""},
		{"optional + missing → anonymous", []router.Option{openapi.OptionalSecurity(), openapi.Security(scheme)}, "", http.StatusOK, ""},
		{"optional + valid → principal", []router.Option{openapi.OptionalSecurity(), openapi.Security(scheme)}, "alice", http.StatusOK, "user-alice"},
		{"optional + invalid → 401", []router.Option{openapi.OptionalSecurity(), openapi.Security(scheme)}, "bad", http.StatusUnauthorized, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newServer(t, tc.opts...)
			code, who := get(t, ts, tc.token)
			if code != tc.wantCode {
				t.Errorf("status = %d, want %d", code, tc.wantCode)
			}
			if who != tc.wantWho {
				t.Errorf("principal = %q, want %q", who, tc.wantWho)
			}
		})
	}
}
