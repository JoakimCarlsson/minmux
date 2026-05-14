// Package corsbrowser drives a real headless Chrome via bonk to verify
// browser-enforced CORS behavior end-to-end. Curl cannot test this because
// curl does not enforce CORS — only browsers do.
//
// Each test sets up two origins on different ports:
//   - apiServer: serves the minmux endpoint we want to call cross-origin
//   - pageServer: serves an HTML page whose script does fetch() to apiServer
//
// bonk then loads pageServer's page and observes whether the cross-origin
// fetch was permitted (title becomes "success:...") or blocked
// (title becomes "blocked:..."). This is the same enforcement path a real
// user would experience.
package corsbrowser_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joakimcarlsson/bonk"
	"github.com/joakimcarlsson/minmux/cors"
	"github.com/joakimcarlsson/minmux/router"
)

func pageHTML(apiURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>pending</title></head>
<body>
<script>
fetch(%q)
  .then(function (r) { return r.json(); })
  .then(function (j) { document.title = "success:" + JSON.stringify(j); })
  .catch(function (e) { document.title = "blocked:" + e.message; });
</script>
</body></html>`, apiURL)
}

func pageHTMLPreflighted(apiURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>pending</title></head>
<body>
<script>
fetch(%q, {
  method: "POST",
  headers: { "Content-Type": "application/json", "X-Custom-Header": "1" },
  body: JSON.stringify({ "x": 1 }),
})
  .then(function (r) { return r.text(); })
  .then(function (j) { document.title = "success:" + j; })
  .catch(function (e) { document.title = "blocked:" + e.message; });
</script>
</body></html>`, apiURL)
}

// runInBrowser launches a headless Chrome via bonk, navigates to pageURL,
// waits until the page sets document.title (signaling the fetch resolved
// or rejected), and returns that title.
func runInBrowser(t *testing.T, pageURL string) string {
	t.Helper()
	b, err := bonk.Launch()
	if err != nil {
		t.Fatalf("bonk launch: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	ctx, err := b.NewContext()
	if err != nil {
		t.Fatalf("new context: %v", err)
	}
	t.Cleanup(func() { _ = ctx.Close() })

	page, err := ctx.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}

	if err := page.Navigate(pageURL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := page.WaitForFunction(`document.title !== "pending"`); err != nil {
		t.Fatalf("wait for title: %v", err)
	}

	title, err := page.Title()
	if err != nil {
		t.Fatalf("read title: %v", err)
	}
	return title
}

// TestBrowser_AllowsWithCORS verifies that when cors.Default() is mounted,
// a real browser permits a cross-origin fetch.
func TestBrowser_AllowsWithCORS(t *testing.T) {
	r := router.New()
	r.Use(cors.Default())
	r.Get("/data", func(c *router.Context) {
		c.JSON(http.StatusOK, map[string]string{"value": "hello"})
	})
	api := httptest.NewServer(r)
	defer api.Close()

	page := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTML(api.URL + "/data")))
		}),
	)
	defer page.Close()

	title := runInBrowser(t, page.URL)
	t.Logf("browser title: %s", title)
	if !strings.HasPrefix(title, "success:") {
		t.Fatalf("expected success, got title %q", title)
	}
	if !strings.Contains(title, `"value":"hello"`) {
		t.Errorf("expected JSON body in response, got %q", title)
	}
}

// TestBrowser_BlocksWithoutCORS verifies that without CORS middleware, the
// browser blocks the cross-origin fetch. This proves the negative — i.e.,
// the previous test's pass actually depends on the middleware doing
// something, not on the request being same-origin.
func TestBrowser_BlocksWithoutCORS(t *testing.T) {
	r := router.New()
	r.Get("/data", func(c *router.Context) {
		c.JSON(http.StatusOK, map[string]string{"value": "hello"})
	})
	api := httptest.NewServer(r)
	defer api.Close()

	page := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTML(api.URL + "/data")))
		}),
	)
	defer page.Close()

	title := runInBrowser(t, page.URL)
	t.Logf("browser title: %s", title)
	if !strings.HasPrefix(title, "blocked:") {
		t.Fatalf(
			"expected browser to block cross-origin fetch without CORS, got %q",
			title,
		)
	}
}

// TestBrowser_PreflightSucceeds verifies that a request requiring a CORS
// preflight (non-simple method + custom header) is permitted by the browser
// when the server correctly responds to the OPTIONS preflight.
func TestBrowser_PreflightSucceeds(t *testing.T) {
	r := router.New()
	r.Use(cors.New(cors.Options{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type", "X-Custom-Header"},
	}))
	r.Post("/data", func(c *router.Context) {
		c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})
	api := httptest.NewServer(r)
	defer api.Close()

	page := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTMLPreflighted(api.URL + "/data")))
		}),
	)
	defer page.Close()

	title := runInBrowser(t, page.URL)
	t.Logf("browser title: %s", title)
	if !strings.HasPrefix(title, "success:") {
		t.Fatalf("expected preflight to succeed, got %q", title)
	}
}

// TestBrowser_PreflightBlocksUnallowedHeader verifies that if the server
// doesn't list a custom header as allowed, the browser blocks the request
// after the failed preflight.
func TestBrowser_PreflightBlocksUnallowedHeader(t *testing.T) {
	r := router.New()
	r.Use(cors.New(cors.Options{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type"}, // X-Custom-Header NOT listed
	}))
	r.Post("/data", func(c *router.Context) {
		c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})
	api := httptest.NewServer(r)
	defer api.Close()

	page := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTMLPreflighted(api.URL + "/data")))
		}),
	)
	defer page.Close()

	title := runInBrowser(t, page.URL)
	t.Logf("browser title: %s", title)
	if !strings.HasPrefix(title, "blocked:") {
		t.Fatalf(
			"expected browser to block request with disallowed preflight header, got %q",
			title,
		)
	}
}
