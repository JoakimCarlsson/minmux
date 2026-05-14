package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func recordMW(name string, log *[]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*log = append(*log, "enter "+name)
			next.ServeHTTP(w, r)
			*log = append(*log, "exit "+name)
		})
	}
}

func TestMiddleware_PerRoute_RunsAroundHandler(t *testing.T) {
	var log []string
	r := New()
	r.Get("/u", func(c *Context) {
		log = append(log, "handler")
		c.NoContent()
	}, Middleware(recordMW("auth", &log)))

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/u", nil))

	if res.Code != http.StatusNoContent {
		t.Fatalf("status: %d", res.Code)
	}
	got := strings.Join(log, ",")
	want := "enter auth,handler,exit auth"
	if got != want {
		t.Errorf("order: got %q, want %q", got, want)
	}
}

func TestMiddleware_PerRoute_ChainsInDeclarationOrder(t *testing.T) {
	var log []string
	r := New()
	r.Get("/u", func(c *Context) {
		log = append(log, "handler")
		c.NoContent()
	}, Middleware(recordMW("a", &log), recordMW("b", &log)))

	r.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/u", nil),
	)

	got := strings.Join(log, ",")
	want := "enter a,enter b,handler,exit b,exit a"
	if got != want {
		t.Errorf("order: got %q, want %q", got, want)
	}
}

func TestMiddleware_GroupCascadesAndRouteAdds(t *testing.T) {
	var log []string
	r := New()
	g := r.Group("/api", Middleware(recordMW("group", &log)))
	g.Get("/u", func(c *Context) {
		log = append(log, "handler")
		c.NoContent()
	}, Middleware(recordMW("route", &log)))

	r.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/u", nil),
	)

	got := strings.Join(log, ",")
	want := "enter group,enter route,handler,exit route,exit group"
	if got != want {
		t.Errorf("order: got %q, want %q", got, want)
	}
}

func TestMiddleware_NoneByDefault(t *testing.T) {
	r := New()
	ep := r.Get("/u", func(c *Context) { c.NoContent() })
	if ep.Middleware != nil {
		t.Errorf(
			"Middleware should be nil when no Middleware option used, got %+v",
			ep.Middleware,
		)
	}
}

func TestMiddleware_CanShortCircuit(t *testing.T) {
	called := false
	deny := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}
	r := New()
	r.Get("/u", func(c *Context) {
		called = true
		c.NoContent()
	}, Middleware(deny))

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/u", nil))

	if res.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", res.Code)
	}
	if called {
		t.Error("handler should not run when middleware short-circuits")
	}
}

func TestMiddleware_OnlyAppliesToOwnRoute(t *testing.T) {
	var log []string
	r := New()
	r.Get("/with", func(c *Context) { c.NoContent() },
		Middleware(recordMW("only-with", &log)))
	r.Get("/without", func(c *Context) { c.NoContent() })

	r.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/without", nil),
	)

	if len(log) != 0 {
		t.Errorf(
			"/without should not invoke middleware bound to /with; log=%v",
			log,
		)
	}
}
