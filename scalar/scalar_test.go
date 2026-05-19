package scalar

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, h http.HandlerFunc) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	h(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res, string(body)
}

func TestHandler_ServesHTML(t *testing.T) {
	res, body := get(t, Handler("/openapi.json"))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(
		ct,
		"text/html",
	) {
		t.Fatalf("Content-Type = %q, want text/html...", ct)
	}
	if !strings.Contains(body, "Scalar.createApiReference") {
		t.Errorf("body missing Scalar.createApiReference call")
	}
	if !strings.Contains(body, `"url":"/openapi.json"`) {
		t.Errorf("body missing url config; got:\n%s", body)
	}
	if !strings.Contains(body, DefaultCDNURL) {
		t.Errorf("body missing default CDN URL")
	}
	if !strings.Contains(body, "<title>"+DefaultTitle+"</title>") {
		t.Errorf("body missing default title; got:\n%s", body)
	}
}

func TestHandlerWith_OptionalFields(t *testing.T) {
	_, body := get(t, HandlerWith(Config{
		SpecURL:  "/openapi.json",
		Title:    "My API",
		Theme:    "moon",
		ProxyURL: "https://proxy.scalar.com",
		CDNURL:   "https://example.com/scalar.js",
	}))

	cases := []string{
		"<title>My API</title>",
		`"theme":"moon"`,
		`"proxyUrl":"https://proxy.scalar.com"`,
		"https://example.com/scalar.js",
	}
	for _, want := range cases {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got:\n%s", want, body)
		}
	}

	if strings.Contains(body, DefaultCDNURL) {
		t.Errorf("custom CDNURL not honored; default still present")
	}
}

func TestHandlerWith_OmitsEmptyOptionalFields(t *testing.T) {
	_, body := get(t, HandlerWith(Config{SpecURL: "/openapi.json"}))

	if strings.Contains(body, `"theme"`) {
		t.Errorf("empty Theme should be omitted")
	}
	if strings.Contains(body, `"proxyUrl"`) {
		t.Errorf("empty ProxyURL should be omitted")
	}
	if strings.Contains(body, `"authentication"`) {
		t.Errorf("nil Authentication should be omitted")
	}
}

func TestHandlerWith_AuthenticationPassthrough(t *testing.T) {
	_, body := get(t, HandlerWith(Config{
		SpecURL: "/openapi.json",
		Authentication: &Authentication{
			PreferredSecurityScheme: "oauth2",
			SecuritySchemes: map[string]SchemeAuth{
				"oauth2": {
					Flows: map[string]FlowAuth{
						"clientCredentials": {
							ClientID:       "svc-reporter",
							SelectedScopes: []string{"metrics:read"},
						},
					},
				},
			},
		},
	}))

	for _, want := range []string{
		`"authentication":{`,
		`"preferredSecurityScheme":"oauth2"`,
		`"x-scalar-client-id":"svc-reporter"`,
		`"selectedScopes":["metrics:read"]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; got:\n%s", want, body)
		}
	}
}

func TestHandler_EscapesScriptInjection(t *testing.T) {
	// A malicious spec URL must not be able to break out of the inline
	// <script> by closing it.
	hostile := `/spec.json"</script><script>alert(1)</script>`
	_, body := get(t, Handler(hostile))

	// encoding/json escapes < and > to < / >, so a literal
	// </script> token must not appear before our closing </script>.
	inline := body
	first := strings.Index(inline, "Scalar.createApiReference")
	if first < 0 {
		t.Fatal("could not locate inline script")
	}
	rest := inline[first:]
	close := strings.Index(rest, "</script>")
	if close < 0 {
		t.Fatal("inline script never closes")
	}
	scriptBody := rest[:close]
	if strings.Contains(scriptBody, "</script>") {
		t.Errorf(
			"inline script body contains </script> literal:\n%s",
			scriptBody,
		)
	}
	if strings.Contains(scriptBody, "<script>") {
		t.Errorf(
			"inline script body contains <script> literal:\n%s",
			scriptBody,
		)
	}
}

func TestHandler_EscapesHTMLInjection(t *testing.T) {
	_, body := get(t, HandlerWith(Config{
		SpecURL: "/openapi.json",
		Title:   "<script>alert(1)</script>",
	}))

	if strings.Contains(body, "<title><script>alert(1)</script></title>") {
		t.Errorf("title not HTML-escaped:\n%s", body)
	}
}
