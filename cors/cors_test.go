package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func do(
	t *testing.T,
	mw func(http.Handler) http.Handler,
	method, origin string,
	extraHeaders map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://example.com/test", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	return rec
}

func TestDefault_AllowsAnyOrigin(t *testing.T) {
	rec := do(t, Default(), "GET", "https://random.example", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin: want *, got %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary: want Origin, got %q", got)
	}
}

func TestDefault_NoOriginIsPassthrough(t *testing.T) {
	rec := do(t, Default(), "GET", "", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("non-CORS request should not get Allow-Origin header")
	}
	if rec.Body.String() != "ok" {
		t.Errorf("non-CORS request body: want ok, got %q", rec.Body.String())
	}
}

func TestExactOriginMatch(t *testing.T) {
	mw := New(Options{AllowOrigins: []string{"https://app.example.com"}})

	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if rec.Header().
		Get("Access-Control-Allow-Origin") !=
		"https://app.example.com" {
		t.Errorf(
			"matched origin: %q",
			rec.Header().Get("Access-Control-Allow-Origin"),
		)
	}

	rec = do(t, mw, "GET", "https://other.example.com", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unmatched origin should not get Allow-Origin, got %q", got)
	}
}

func TestSubdomainWildcard(t *testing.T) {
	mw := New(Options{AllowOrigins: []string{"*.example.com"}})

	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if rec.Header().
		Get("Access-Control-Allow-Origin") !=
		"https://app.example.com" {
		t.Errorf(
			"subdomain match: %q",
			rec.Header().Get("Access-Control-Allow-Origin"),
		)
	}

	rec = do(t, mw, "GET", "https://app.other.com", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("non-matching domain should not get Allow-Origin, got %q", got)
	}
}

func TestCredentials_WildcardEchoesOrigin(t *testing.T) {
	mw := New(Options{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
	})

	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if got := rec.Header().
		Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf(
			"Allow-Origin with credentials must echo origin, not *, got %q",
			got,
		)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf(
			"Allow-Credentials: want true, got %q",
			rec.Header().Get("Access-Control-Allow-Credentials"),
		)
	}
}

func TestCredentials_NoStarOriginWhenCredentialsOff(t *testing.T) {
	mw := New(Options{AllowOrigins: []string{"*"}})
	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("without credentials, * should be sent, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf(
			"Allow-Credentials should be omitted, got %q",
			rec.Header().Get("Access-Control-Allow-Credentials"),
		)
	}
}

func TestPreflight(t *testing.T) {
	mw := New(Options{
		AllowOrigins: []string{"https://app.example.com"},
		AllowMethods: []string{"GET", "POST", "DELETE"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:       3600,
	})

	rec := do(t, mw, "OPTIONS", "https://app.example.com", map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "Authorization",
	})

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status: want 204, got %d", rec.Code)
	}
	if got := rec.Header().
		Get("Access-Control-Allow-Methods"); got != "GET, POST, DELETE" {
		t.Errorf("Allow-Methods: %q", got)
	}
	if got := rec.Header().
		Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Errorf("Allow-Headers: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("Max-Age: %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body should be empty, got %q", rec.Body.String())
	}
}

func TestPreflight_NotARealPreflight(t *testing.T) {
	// OPTIONS without Access-Control-Request-Method is not a CORS preflight;
	// it should pass through to the handler.
	mw := New(Options{AllowOrigins: []string{"https://app.example.com"}})

	rec := do(t, mw, "OPTIONS", "https://app.example.com", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("plain OPTIONS should pass through, got %d", rec.Code)
	}
}

func TestExposeHeaders(t *testing.T) {
	mw := New(Options{
		AllowOrigins:  []string{"*"},
		ExposeHeaders: []string{"X-Total-Count", "X-Request-Id"},
	})

	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if got := rec.Header().
		Get("Access-Control-Expose-Headers"); got != "X-Total-Count, X-Request-Id" {
		t.Errorf("Expose-Headers: %q", got)
	}
}

func TestAllowOriginFunc(t *testing.T) {
	mw := New(Options{
		AllowOriginFunc: func(origin string) bool {
			return origin == "https://yes.example.com"
		},
	})

	rec := do(t, mw, "GET", "https://yes.example.com", nil)
	if got := rec.Header().
		Get("Access-Control-Allow-Origin"); got != "https://yes.example.com" {
		t.Errorf("func-allowed origin: %q", got)
	}

	rec = do(t, mw, "GET", "https://no.example.com", nil)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf(
			"func-rejected origin should not get Allow-Origin, got %q",
			got,
		)
	}
}

func TestCaseInsensitiveOriginMatch(t *testing.T) {
	mw := New(Options{AllowOrigins: []string{"https://App.Example.com"}})
	rec := do(t, mw, "GET", "https://app.example.com", nil)
	if got := rec.Header().
		Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("case-insensitive match failed: %q", got)
	}
}

func TestDefaultMethodsWhenNoneProvided(t *testing.T) {
	mw := New(Options{AllowOrigins: []string{"*"}})
	rec := do(t, mw, "OPTIONS", "https://app.example.com", map[string]string{
		"Access-Control-Request-Method": "GET",
	})
	got := rec.Header().Get("Access-Control-Allow-Methods")
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !contains(got, m) {
			t.Errorf("default methods missing %s, got %q", m, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
