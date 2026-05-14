package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type benchPathParams struct {
	ID int `path:"id"`
}

type benchBody struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type benchBodyParams struct {
	Body benchBody `body:""`
}

type benchUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func BenchmarkMinmux_NoParams(b *testing.B) {
	r := router.New()
	r.Get("/hello", func(c *router.Context) {
		c.JSON(http.StatusOK, "world")
	})
	req := httptest.NewRequest("GET", "/hello", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rec.Body.Reset()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMinmux_PathParam(b *testing.B) {
	r := router.New()
	r.Get("/u/{id}", func(c *router.Context, p benchPathParams) {
		c.JSON(http.StatusOK, benchUser{ID: p.ID, Name: "n"})
	})
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rec.Body.Reset()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkMinmux_PostBody(b *testing.B) {
	r := router.New()
	r.Post("/u", func(c *router.Context, p benchBodyParams) {
		c.JSON(http.StatusOK, benchUser{ID: 1, Name: p.Body.Name})
	})
	body := `{"name":"alice","age":30}`

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest("POST", "/u", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkBaseline_NoParams(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode("world")
	})
	req := httptest.NewRequest("GET", "/hello", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rec.Body.Reset()
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkBaseline_PathParam(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /u/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(benchUser{ID: 1, Name: "n"})
	})
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rec.Body.Reset()
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkBaseline_PostBody(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /u", func(w http.ResponseWriter, r *http.Request) {
		var body benchBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(benchUser{ID: 1, Name: body.Name})
	})
	body := `{"name":"alice","age":30}`

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest("POST", "/u", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}
}
