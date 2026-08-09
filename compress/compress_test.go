package compress

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// body is long enough and repetitive enough to be worth compressing.
var body = strings.Repeat("the quick brown fox jumps over the lazy dog. ", 200)

func do(
	t *testing.T,
	mw func(http.Handler) http.Handler,
	acceptEncoding string,
	h http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, req)
	return rec
}

func jsonHandler(payload string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a gzip stream: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read gzip stream: %v", err)
	}
	return string(out)
}

func inflate(t *testing.T, b []byte) string {
	t.Helper()
	out, err := io.ReadAll(flate.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatalf("read deflate stream: %v", err)
	}
	return string(out)
}

func TestDefault_CompressesLargeJSON(t *testing.T) {
	rec := do(t, Default(), "gzip", jsonHandler(body))

	if got := rec.Header().Get("Content-Encoding"); got != EncodingGzip {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Fatalf("body round-trip mismatch: %d bytes back", len(got))
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("compressed to %d bytes, from %d", rec.Body.Len(), len(body))
	}
}

func TestDefault_SetsVaryAndDropsContentLength(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	})

	if v := rec.Header().Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want it to include Accept-Encoding", v)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, want it dropped", got)
	}
}

func TestDefault_LeavesSmallBodyAlone(t *testing.T) {
	rec := do(t, Default(), "gzip", jsonHandler(`{"error":"nope"}`))

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if got := rec.Body.String(); got != `{"error":"nope"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestDefault_PreservesStatusOnSmallBody(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"unavailable"}`)
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"unavailable"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestDefault_PreservesStatusOnCompressedBody(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, body)
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Fatalf("body round-trip mismatch")
	}
}

func TestDefault_SkipsWithoutAcceptEncoding(t *testing.T) {
	rec := do(t, Default(), "", jsonHandler(body))

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("body was altered")
	}
}

// A shared cache keys on Vary. An eligible response that happens to go out
// uncompressed still varies by Accept-Encoding, or the cache can hand it to a
// client whose encoding would have produced a different body.
func TestDefault_VariesEvenWhenNotCompressed(t *testing.T) {
	for _, c := range []struct {
		name           string
		acceptEncoding string
		handler        http.HandlerFunc
	}{
		{"no accept-encoding", "", jsonHandler(body)},
		{"refused encoding", "gzip;q=0, deflate;q=0", jsonHandler(body)},
		{"under min size", "gzip", jsonHandler("{}")},
	} {
		rec := do(t, Default(), c.acceptEncoding, c.handler)
		v := rec.Header().Get("Vary")
		if !strings.Contains(v, "Accept-Encoding") {
			t.Errorf("%s: Vary = %q, want it to include Accept-Encoding",
				c.name, v)
		}
	}
}

// The handler writes headers and then a body, so the wrapper's decision point
// is reached twice. It must settle once.
func TestDefault_VaryNotDuplicated(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Values("Vary"); len(got) != 1 {
		t.Fatalf("Vary = %v, want exactly one entry", got)
	}
}

// An uncompressible response must not collect a Vary it does not need.
func TestDefault_NoVaryOnUncompressibleType(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Get("Vary"); got != "" {
		t.Fatalf("Vary = %q, want none", got)
	}
}

func TestDefault_SkipsAlreadyEncodedBody(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, body)
	})

	if rec.Body.String() != body {
		t.Fatalf("pre-encoded body was re-encoded: %d bytes", rec.Body.Len())
	}
}

func TestDefault_SkipsUncompressibleType(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("body was altered")
	}
}

func TestDefault_SkipsPartialContent(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Range", "bytes 0-99/"+strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("body was altered")
	}
}

func TestDefault_HonoursMediaTypeParameters(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})

	if got := rec.Header().Get("Content-Encoding"); got != EncodingGzip {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
}

func TestDefault_EmptyBodyStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusNoContent,
		http.StatusNotModified,
	} {
		rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
		})
		if rec.Code != status {
			t.Fatalf("status = %d, want %d", rec.Code, status)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("%d: Content-Encoding = %q, want none", status, got)
		}
	}
}

func TestDefault_HandlerWritesNothing(t *testing.T) {
	rec := do(t, Default(), "gzip", func(http.ResponseWriter, *http.Request) {})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

func TestDefault_FlushSendsStreamUncompressed(t *testing.T) {
	rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: two\n\n")
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if got := rec.Body.String(); got != "data: one\n\ndata: two\n\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestNew_Deflate(t *testing.T) {
	rec := do(t, Default(), "deflate", jsonHandler(body))

	if got := rec.Header().Get("Content-Encoding"); got != EncodingDeflate {
		t.Fatalf("Content-Encoding = %q, want deflate", got)
	}
	if got := inflate(t, rec.Body.Bytes()); got != body {
		t.Fatalf("body round-trip mismatch")
	}
}

func TestNew_MinSizeNegativeCompressesEverything(t *testing.T) {
	rec := do(t, New(Options{MinSize: -1}), "gzip", jsonHandler("{}"))

	if got := rec.Header().Get("Content-Encoding"); got != EncodingGzip {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := gunzip(t, rec.Body.Bytes()); got != "{}" {
		t.Fatalf("body = %q", got)
	}
}

// Structured syntax suffixes (RFC 6839) are a whole family that cannot be
// enumerated, and one member is what minmux's own router serves errors as.
func TestDefault_CompressesStructuredSuffixes(t *testing.T) {
	for _, mediaType := range []string{
		"application/problem+json",
		"application/ld+json",
		"application/vnd.api+json",
		"application/geo+json",
		"application/manifest+json",
		"image/svg+xml",
		"application/xhtml+xml",
		"application/atom+xml",
	} {
		rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", mediaType)
			_, _ = io.WriteString(w, body)
		})
		if got := rec.Header().Get("Content-Encoding"); got != EncodingGzip {
			t.Errorf("%s: Content-Encoding = %q, want gzip", mediaType, got)
		}
	}
}

// A suffix is not a licence to compress anything carrying a "+": the archive
// suffixes name payloads that are already compressed.
func TestDefault_SkipsCompressedSuffixes(t *testing.T) {
	for _, mediaType := range []string{
		"application/epub+zip",
		"application/vnd.oasis.opendocument.text",
	} {
		rec := do(t, Default(), "gzip", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", mediaType)
			_, _ = io.WriteString(w, body)
		})
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("%s: Content-Encoding = %q, want none", mediaType, got)
		}
	}
}

// The suffix rule belongs to DefaultTypes. A Types list that did not ask for
// it does not get it.
func TestTypes_SuffixIsNotImplied(t *testing.T) {
	mw := New(Options{Types: []string{"application/json"}})
	rec := do(t, mw, "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		_, _ = io.WriteString(w, body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("explicit Types: Content-Encoding = %q, want none", got)
	}
}

func TestNew_Types(t *testing.T) {
	mw := New(Options{Types: []string{"text/csv"}})

	rec := do(t, mw, "gzip", jsonHandler(body))
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("json: Content-Encoding = %q, want none", got)
	}

	rec = do(t, mw, "gzip", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, body)
	})
	if got := rec.Header().Get("Content-Encoding"); got != EncodingGzip {
		t.Fatalf("csv: Content-Encoding = %q, want gzip", got)
	}
}

func TestNew_Skip(t *testing.T) {
	mw := New(Options{Skip: func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/test")
	}})

	rec := do(t, mw, "gzip", jsonHandler(body))
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if rec.Body.String() != body {
		t.Fatalf("body was altered")
	}
}

func TestNew_InvalidLevelFallsBackToDefault(t *testing.T) {
	rec := do(t, New(Options{Level: 42}), "gzip", jsonHandler(body))

	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Fatalf("body round-trip mismatch")
	}
}

func TestNew_LevelsRoundTrip(t *testing.T) {
	for _, level := range []int{
		flate.BestSpeed,
		flate.BestCompression,
		flate.HuffmanOnly,
	} {
		rec := do(t, New(Options{Level: level}), "gzip", jsonHandler(body))
		if got := gunzip(t, rec.Body.Bytes()); got != body {
			t.Fatalf("level %d: body round-trip mismatch", level)
		}
	}
}

func TestNegotiate(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"", ""},
		{"gzip", EncodingGzip},
		{"deflate", EncodingDeflate},
		{"gzip, deflate", EncodingGzip},
		{"deflate, gzip", EncodingGzip},
		{"GZIP", EncodingGzip},
		{" gzip , deflate ", EncodingGzip},
		{"br", ""},
		{"identity", ""},
		{"gzip;q=0", ""},
		{"gzip;q=0, deflate", EncodingDeflate},
		{"gzip;q=0.5, deflate;q=0.9", EncodingDeflate},
		{"gzip;q=1.0, deflate;q=1.0", EncodingGzip},
		{"*", EncodingGzip},
		{"*;q=0", ""},
		{"gzip;q=0, *", EncodingDeflate},
		{"br;q=1.0, gzip;q=0.8", EncodingGzip},
		{"gzip;q=nonsense", ""},
	}
	for _, c := range cases {
		if got := Negotiate(c.header); got != c.want {
			t.Errorf("Negotiate(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

func TestPool_ReusedWriterProducesValidStream(t *testing.T) {
	mw := Default()
	for i := range 3 {
		rec := do(t, mw, "gzip", jsonHandler(body))
		if got := gunzip(t, rec.Body.Bytes()); got != body {
			t.Fatalf("request %d: body round-trip mismatch", i)
		}
	}
}
