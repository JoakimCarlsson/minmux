package router_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type bindHookParams struct {
	ID    int `path:"id"`
	Limit int `          query:"limit"`
	Trace int `                        header:"X-Trace"`
}

type bindHookBody struct {
	Name string `json:"name"`
}

type bindHookBodyParams struct {
	Body bindHookBody `body:""`
}

// serveBind sends a request with the given method, target, body and headers
// to r and returns the recorded response.
func serveBind(
	r *router.Router,
	method, target, body string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// newBindHookRouter returns a router whose WithBindError hook records the
// error it receives in *got and answers with want, carrying the parameter
// and body routes the bind hook tests exercise.
func newBindHookRouter(
	got *error,
	want *router.ProblemDetails,
) *router.Router {
	r := router.New(router.WithBindError(
		func(_ *http.Request, err error) *router.ProblemDetails {
			*got = err
			return want
		},
	))
	r.Get("/items/{id}", func(c *router.Context, p bindHookParams) {
		c.NoContent()
	})
	r.Post("/items", func(c *router.Context, p bindHookBodyParams) {
		c.NoContent()
	})
	return r
}

// TestWithBindError_DecidesEveryBindingFailure checks that the hook decides
// the status and body for path, query, header and body failures, and that
// the error it receives names the source and wraps the cause.
func TestWithBindError_DecidesEveryBindingFailure(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		target     string
		body       string
		headers    map[string]string
		wantSource string
		wantName   string
	}{
		{"path", "GET", "/items/abc", "", nil, "path", "id"},
		{"query", "GET", "/items/1?limit=x", "", nil, "query", "limit"},
		{
			"header",
			"GET",
			"/items/1",
			"",
			map[string]string{"X-Trace": "x"},
			"header",
			"X-Trace",
		},
		{
			"body",
			"POST",
			"/items",
			"{",
			map[string]string{"Content-Type": "application/json"},
			"body",
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got error
			r := newBindHookRouter(&got, &router.ProblemDetails{
				Type:   "invalid_" + tc.name,
				Status: http.StatusUnprocessableEntity,
			})
			rec := serveBind(r, tc.method, tc.target, tc.body, tc.headers)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			want := `{"type":"invalid_` + tc.name + `","status":422}`
			if body := strings.TrimSpace(rec.Body.String()); body != want {
				t.Errorf("body = %s, want %s", body, want)
			}
			var be *router.BindError
			if !errors.As(got, &be) {
				t.Fatalf("hook error = %T %v, want *router.BindError", got, got)
			}
			if be.Source != tc.wantSource || be.Name != tc.wantName {
				t.Errorf(
					"source, name = %q, %q, want %q, %q",
					be.Source, be.Name, tc.wantSource, tc.wantName,
				)
			}
			if be.Problem().Status != http.StatusBadRequest {
				t.Errorf("default problem status = %d", be.Problem().Status)
			}
		})
	}
}

// TestWithBindError_TellsBodyFromParameter checks that the hook can tell a
// body that is not valid JSON from a parameter that does not parse by the
// cause the error wraps.
func TestWithBindError_TellsBodyFromParameter(t *testing.T) {
	var got error
	r := newBindHookRouter(&got, &router.ProblemDetails{Status: 400})

	serveBind(r, "POST", "/items", "{]", map[string]string{
		"Content-Type": "application/json",
	})
	var syntaxErr *json.SyntaxError
	if !errors.As(got, &syntaxErr) {
		t.Errorf("body error = %v, want a *json.SyntaxError cause", got)
	}

	serveBind(r, "GET", "/items/abc", "", nil)
	var numErr *strconv.NumError
	if !errors.As(got, &numErr) {
		t.Errorf("path error = %v, want a *strconv.NumError cause", got)
	}
}

// TestWithBindError_ExactBody checks the issue's runnable example: posting
// "{" to a JSON body route answers exactly the problem the hook returns.
func TestWithBindError_ExactBody(t *testing.T) {
	r := router.New(router.WithBindError(
		func(*http.Request, error) *router.ProblemDetails {
			return &router.ProblemDetails{Type: "invalid_body", Status: 400}
		},
	))
	r.Post("/items", func(c *router.Context, p bindHookBodyParams) {
		c.NoContent()
	})
	rec := serveBind(r, "POST", "/items", "{", map[string]string{
		"Content-Type": "application/json",
	})
	if got := rec.Body.String(); got != "{\"type\":\"invalid_body\",\"status\":400}\n" {
		t.Errorf("body = %q", got)
	}
}

// TestWithBindError_NilFallsBackToDefault checks that a hook returning nil
// writes the default problem, the same bytes as a router without a hook.
func TestWithBindError_NilFallsBackToDefault(t *testing.T) {
	hooked := router.New(router.WithBindError(
		func(*http.Request, error) *router.ProblemDetails { return nil },
	))
	plain := router.New()
	for _, r := range []*router.Router{hooked, plain} {
		r.Post("/items", func(c *router.Context, p bindHookBodyParams) {
			c.NoContent()
		})
	}
	want := "{\"title\":\"Bad Request\",\"status\":400,\"detail\":\"body: unexpected EOF\"}\n"
	for name, r := range map[string]*router.Router{"hooked": hooked, "plain": plain} {
		rec := serveBind(r, "POST", "/items", "{", map[string]string{
			"Content-Type": "application/json",
		})
		body, _ := io.ReadAll(rec.Body)
		if rec.Code != http.StatusBadRequest || string(body) != want {
			t.Errorf("%s: %d %q, want 400 %q", name, rec.Code, body, want)
		}
	}
}

// TestWithBindError_ZeroStatusWritesBadRequest checks that a hook problem
// without a status is written as a 400 instead of panicking in WriteHeader.
func TestWithBindError_ZeroStatusWritesBadRequest(t *testing.T) {
	r := router.New(router.WithBindError(
		func(*http.Request, error) *router.ProblemDetails {
			return &router.ProblemDetails{Type: "invalid_body"}
		},
	))
	r.Post("/items", func(c *router.Context, p bindHookBodyParams) {
		c.NoContent()
	})
	rec := serveBind(r, "POST", "/items", "{", map[string]string{
		"Content-Type": "application/json",
	})
	want := "{\"type\":\"invalid_body\",\"status\":400}\n"
	if rec.Code != http.StatusBadRequest || rec.Body.String() != want {
		t.Errorf("got %d %q, want 400 %q", rec.Code, rec.Body, want)
	}
}
