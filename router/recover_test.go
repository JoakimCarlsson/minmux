package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecover_PanicConvertsTo500ProblemDetails(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	r := New()
	r.Use(RecoverWith(logger))
	r.Get("/boom", func(c *Context) {
		panic("kaboom")
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: %q", ct)
	}

	var pd ProblemDetails
	if err := json.Unmarshal(res.Body.Bytes(), &pd); err != nil {
		t.Fatalf("body not ProblemDetails JSON: %v\n%s", err, res.Body)
	}
	if pd.Status != 500 || pd.Title != "Internal Server Error" {
		t.Errorf("problem: %+v", pd)
	}

	logged := buf.String()
	if !strings.Contains(logged, "kaboom") {
		t.Errorf("panic value not logged: %s", logged)
	}
	if !strings.Contains(logged, "GET /boom") {
		t.Errorf("request line not logged: %s", logged)
	}
	if !strings.Contains(logged, ".go") {
		t.Errorf("stack trace not logged: %s", logged)
	}
}

func TestRecover_PassesThroughNonPanic(t *testing.T) {
	r := New()
	r.Use(Recover())
	r.Get("/ok", func(c *Context) {
		c.JSON(http.StatusOK, map[string]string{"hello": "world"})
	})

	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("status: %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"hello":"world"`) {
		t.Errorf("body: %s", res.Body)
	}
}

func TestRecover_RepanicsErrAbortHandler(t *testing.T) {
	r := New()
	r.Use(Recover())
	r.Get("/abort", func(c *Context) {
		panic(http.ErrAbortHandler)
	})

	// http.ErrAbortHandler should bubble up so net/http handles it; with
	// httptest, ServeHTTP itself panics. Catch and verify.
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected http.ErrAbortHandler to re-panic")
		}
		if !errors.Is(rec.(error), http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", rec)
		}
	}()
	r.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/abort", nil),
	)
}

func TestRecover_NilLoggerFallsBackToDefault(t *testing.T) {
	r := New()
	r.Use(RecoverWith(nil))
	r.Get("/boom", func(c *Context) { panic("x") })

	res := httptest.NewRecorder()
	// Must not itself panic; uses log.Default() under the hood.
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if res.Code != http.StatusInternalServerError {
		t.Errorf("status: %d", res.Code)
	}
}
