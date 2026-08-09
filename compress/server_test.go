package compress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// raw fetches over a real connection without the transport's own
// Accept-Encoding handling, so the test sees exactly what went on the wire.
func raw(t *testing.T, url, acceptEncoding string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	client := &http.Client{
		Transport: &http.Transport{DisableCompression: true},
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return res
}

func TestServer_OverTheWire(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/big", jsonHandler(body))
	mux.HandleFunc("/small", jsonHandler(`{"error":"nope"}`))
	srv := httptest.NewServer(Default()(mux))
	defer srv.Close()

	res := raw(t, srv.URL+"/big", "gzip, deflate")
	defer func() { _ = res.Body.Close() }()
	if got := res.Header.Get("Content-Encoding"); got != EncodingGzip {
		t.Fatalf("big: Content-Encoding = %q, want gzip", got)
	}
	wire, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("big: read: %v", err)
	}
	if got := gunzip(t, wire); got != body {
		t.Fatalf("big: body round-trip mismatch")
	}
	t.Logf("gzip: %d bytes on the wire, from %d", len(wire), len(body))

	plain := raw(t, srv.URL+"/big", "")
	defer func() { _ = plain.Body.Close() }()
	if got := plain.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("identity: Content-Encoding = %q, want none", got)
	}
	out, err := io.ReadAll(plain.Body)
	if err != nil {
		t.Fatalf("identity: read: %v", err)
	}
	if string(out) != body {
		t.Fatalf("identity: body was altered")
	}
	if len(wire) >= len(out) {
		t.Fatalf("gzip (%d) did not beat identity (%d)", len(wire), len(out))
	}

	small := raw(t, srv.URL+"/small", "gzip")
	defer func() { _ = small.Body.Close() }()
	if got := small.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("small: Content-Encoding = %q, want none", got)
	}
}

// TestServer_TransparentToDefaultClient is the property that matters most in
// practice: Go's own transport asks for gzip and unwraps it, so a caller that
// configured nothing sees the plain body and no encoding header at all.
func TestServer_TransparentToDefaultClient(t *testing.T) {
	srv := httptest.NewServer(Default()(http.HandlerFunc(jsonHandler(body))))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != body {
		t.Fatalf("body mismatch: %d bytes back, want %d", len(out), len(body))
	}
	if got := res.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want it consumed by the transport", got)
	}
	if v := res.Header.Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want it to include Accept-Encoding", v)
	}
}
