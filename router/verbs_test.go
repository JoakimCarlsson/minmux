package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_Options_DispatchesTypedHandler(t *testing.T) {
	r := New()
	r.Options("/widgets", func(c *Context) {
		c.Header("Allow", "GET, POST, OPTIONS")
		c.NoContent()
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodOptions, "/widgets", nil))

	if res.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Allow"); got != "GET, POST, OPTIONS" {
		t.Errorf("Allow: got %q", got)
	}
}

func TestRouter_Head_DispatchesTypedHandler(t *testing.T) {
	r := New()
	r.Head("/widgets", func(c *Context) {
		c.Header("X-Count", "42")
		c.NoContent()
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodHead, "/widgets", nil))

	if res.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("X-Count"); got != "42" {
		t.Errorf("X-Count: got %q", got)
	}
}

func TestGroup_OptionsHead_ScopedToPrefix(t *testing.T) {
	r := New()
	g := r.Group("/api")
	g.Options("/widgets", func(c *Context) { c.NoContent() })
	g.Head("/widgets", func(c *Context) { c.NoContent() })

	for _, m := range []string{http.MethodOptions, http.MethodHead} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(m, "/api/widgets", nil))
		if res.Code != http.StatusNoContent {
			t.Errorf("%s /api/widgets: status %d", m, res.Code)
		}
	}

	eps := r.Endpoints()
	var sawOpt, sawHead bool
	for _, ep := range eps {
		if ep.Method == http.MethodOptions && ep.Path == "/api/widgets" {
			sawOpt = true
		}
		if ep.Method == http.MethodHead && ep.Path == "/api/widgets" {
			sawHead = true
		}
	}
	if !sawOpt || !sawHead {
		t.Errorf("endpoints missing OPTIONS=%v HEAD=%v", sawOpt, sawHead)
	}
}
