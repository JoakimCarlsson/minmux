package auth_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joakimcarlsson/minmux/auth"
	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// newProblemServer returns a server with one route requiring the test scheme,
// guarded by an Authenticator built with the given Problem hook.
func newProblemServer(
	t *testing.T,
	hook func(*http.Request, *router.ProblemDetails) *router.ProblemDetails,
) *httptest.Server {
	t.Helper()
	r := router.New()
	authn := auth.New(r, auth.Config{
		Verifiers: map[string]auth.Verifier{scheme: testVerifier},
		Problem:   hook,
	})
	r.Use(authn.Middleware())
	r.Get("/x", func(c *router.Context) {
		c.NoContent()
	}, openapi.Security(scheme))
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

// getRaw sends GET /x with token as X-Token (none when empty) and returns the
// status, Content-Type and raw body.
func getRaw(
	t *testing.T,
	ts *httptest.Server,
	token string,
) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/x", nil)
	if token != "" {
		req.Header.Set("X-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// outcomes lists the three authenticator failures with the token that
// triggers each and the default problem written for it.
var outcomes = []struct {
	name     string
	token    string
	wantType string
	wantBody string
}{
	{
		"missing credential",
		"",
		auth.TypeUnauthenticated,
		`{"type":"unauthenticated","title":"Unauthorized","status":401,"detail":"authentication required"}` + "\n",
	},
	{
		"invalid credential",
		"bad",
		auth.TypeInvalidCredentials,
		`{"type":"invalid_credentials","title":"Unauthorized","status":401,"detail":"invalid or missing credentials"}` + "\n",
	},
	{
		"forbidden",
		"forbidden",
		auth.TypeForbidden,
		`{"type":"forbidden","title":"Forbidden","status":403,"detail":"insufficient permissions"}` + "\n",
	},
}

// TestDefaultProblemTypes checks that without a hook each outcome writes its
// default problem carrying the outcome's type.
func TestDefaultProblemTypes(t *testing.T) {
	ts := newProblemServer(t, nil)
	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			_, ct, body := getRaw(t, ts, tc.token)
			if ct != "application/problem+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if body != tc.wantBody {
				t.Errorf("body = %s, want %s", body, tc.wantBody)
			}
		})
	}
}

// TestProblemHook checks that the hook receives the default problem for each
// outcome and that the status and body it returns are the ones written.
func TestProblemHook(t *testing.T) {
	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			var gotType string
			ts := newProblemServer(t, func(
				_ *http.Request,
				p *router.ProblemDetails,
			) *router.ProblemDetails {
				gotType = p.Type
				return &router.ProblemDetails{Type: p.Type, Status: 418}
			})
			code, ct, body := getRaw(t, ts, tc.token)
			if gotType != tc.wantType {
				t.Errorf("hook type = %q, want %q", gotType, tc.wantType)
			}
			if code != 418 {
				t.Errorf("status = %d, want 418", code)
			}
			if ct != "application/problem+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			want := `{"type":"` + tc.wantType + `","status":418}` + "\n"
			if body != want {
				t.Errorf("body = %s, want %s", body, want)
			}
		})
	}
}

// TestProblemHookExactBody checks the issue's runnable example: a hook
// returning a bare unauthenticated problem answers a request without a
// credential with exactly that problem.
func TestProblemHookExactBody(t *testing.T) {
	ts := newProblemServer(t, func(
		*http.Request,
		*router.ProblemDetails,
	) *router.ProblemDetails {
		return &router.ProblemDetails{Type: "unauthenticated", Status: 401}
	})
	code, _, body := getRaw(t, ts, "")
	if code != http.StatusUnauthorized ||
		body != "{\"type\":\"unauthenticated\",\"status\":401}\n" {
		t.Errorf("got %d %q", code, body)
	}
}

// TestProblemHookNilFallsBack checks that a hook returning nil writes the
// default problem.
func TestProblemHookNilFallsBack(t *testing.T) {
	ts := newProblemServer(t, func(
		*http.Request,
		*router.ProblemDetails,
	) *router.ProblemDetails {
		return nil
	})
	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			_, _, body := getRaw(t, ts, tc.token)
			if body != tc.wantBody {
				t.Errorf("body = %s, want %s", body, tc.wantBody)
			}
		})
	}
}

// TestProblemHookZeroStatusKeepsDefault checks that a hook problem without a
// status is written with the default problem's status for each outcome.
func TestProblemHookZeroStatusKeepsDefault(t *testing.T) {
	ts := newProblemServer(t, func(
		_ *http.Request,
		p *router.ProblemDetails,
	) *router.ProblemDetails {
		return &router.ProblemDetails{Type: p.Type}
	})
	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			code, _, body := getRaw(t, ts, tc.token)
			wantCode := http.StatusUnauthorized
			if tc.wantType == auth.TypeForbidden {
				wantCode = http.StatusForbidden
			}
			want := fmt.Sprintf(
				"{\"type\":%q,\"status\":%d}\n", tc.wantType, wantCode,
			)
			if code != wantCode || body != want {
				t.Errorf("got %d %q, want %d %q", code, body, wantCode, want)
			}
		})
	}
}
