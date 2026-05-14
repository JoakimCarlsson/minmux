package router_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type streamItem struct {
	N int    `json:"n"`
	S string `json:"s"`
}

// flushRecorder is an httptest.ResponseRecorder that also implements
// http.Flusher, so stream writers can verify Flush is called.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (f *flushRecorder) Flush() { f.flushes++ }

func TestStream_JSONLFraming(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/jsonl")
		defer w.Close()
		_ = w.Send(streamItem{N: 1, S: "a"})
		_ = w.Send(streamItem{N: 2, S: "b"})
	})

	rec := newFlushRecorder()
	req := httptest.NewRequest("GET", "/s", nil)
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/jsonl" {
		t.Errorf("Content-Type: %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control: %q", got)
	}
	body := rec.Body.String()
	want := "{\"n\":1,\"s\":\"a\"}\n{\"n\":2,\"s\":\"b\"}\n"
	if body != want {
		t.Errorf("body: want %q, got %q", want, body)
	}
	if rec.flushes < 2 {
		t.Errorf("expected at least 2 flushes, got %d", rec.flushes)
	}
}

func TestStream_JSONSeqFraming(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/json-seq")
		defer w.Close()
		_ = w.Send(streamItem{N: 1, S: "a"})
		_ = w.Send(streamItem{N: 2, S: "b"})
	})

	rec := newFlushRecorder()
	req := httptest.NewRequest("GET", "/s", nil)
	r.ServeHTTP(rec, req)

	body := rec.Body.String()
	want := "\x1e{\"n\":1,\"s\":\"a\"}\n\x1e{\"n\":2,\"s\":\"b\"}\n"
	if body != want {
		t.Errorf("body: want %q, got %q", want, body)
	}
}

func TestStream_NDJSONFraming(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/x-ndjson")
		defer w.Close()
		_ = w.Send(streamItem{N: 1, S: "x"})
	})

	rec := newFlushRecorder()
	req := httptest.NewRequest("GET", "/s", nil)
	r.ServeHTTP(rec, req)
	if rec.Body.String() != "{\"n\":1,\"s\":\"x\"}\n" {
		t.Errorf("body: %q", rec.Body.String())
	}
}

func TestStream_SendAfterCloseReturnsError(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/jsonl")
		_ = w.Close()
		if err := w.Send(streamItem{N: 1}); err != router.ErrStreamClosed {
			t.Errorf("send-after-close: want ErrStreamClosed, got %v", err)
		}
	})
	r.ServeHTTP(newFlushRecorder(), httptest.NewRequest("GET", "/s", nil))
}

func TestStream_CancelledContextStopsSend(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/jsonl")
		defer w.Close()
		_ = w.Send(streamItem{N: 1})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/s", nil).WithContext(ctx)
	rec := newFlushRecorder()
	r.ServeHTTP(rec, req)

	if rec.Body.Len() != 0 {
		t.Errorf("expected no body after cancelled context, got %q", rec.Body)
	}
}

func TestSSE_SimpleFraming(t *testing.T) {
	r := router.New()
	r.Get("/sse", func(c *router.Context) {
		sse := c.SSE(http.StatusOK)
		defer sse.Close()
		_ = sse.Send(router.SSEEvent{
			ID:    "1",
			Event: "token",
			Data:  "hello",
		})
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/sse", nil))

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type: %q", got)
	}
	body := rec.Body.String()
	want := "id: 1\nevent: token\ndata: hello\n\n"
	if body != want {
		t.Errorf("body: want %q, got %q", want, body)
	}
}

func TestSSE_MultilineDataIsSplit(t *testing.T) {
	r := router.New()
	r.Get("/sse", func(c *router.Context) {
		sse := c.SSE(http.StatusOK)
		defer sse.Close()
		_ = sse.Send(router.SSEEvent{
			Event: "message",
			Data:  "line one\nline two",
		})
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/sse", nil))
	want := "event: message\ndata: line one\ndata: line two\n\n"
	if rec.Body.String() != want {
		t.Errorf("body: %q", rec.Body.String())
	}
}

func TestSSE_JSONDataEncodesAsJSON(t *testing.T) {
	r := router.New()
	r.Get("/sse", func(c *router.Context) {
		sse := c.SSE(http.StatusOK)
		defer sse.Close()
		_ = sse.Send(router.SSEEvent{
			Event: "token",
			Data:  streamItem{N: 7, S: "z"},
		})
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/sse", nil))
	want := "event: token\ndata: {\"n\":7,\"s\":\"z\"}\n\n"
	if rec.Body.String() != want {
		t.Errorf("body: %q", rec.Body.String())
	}
}

func TestSSE_RetryAndComment(t *testing.T) {
	r := router.New()
	r.Get("/sse", func(c *router.Context) {
		sse := c.SSE(http.StatusOK)
		defer sse.Close()
		_ = sse.Send(
			router.SSEEvent{Comment: "warmup", Retry: 1000, Data: "ok"},
		)
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/sse", nil))
	want := ": warmup\nretry: 1000\ndata: ok\n\n"
	if rec.Body.String() != want {
		t.Errorf("body: %q", rec.Body.String())
	}
}

func TestMultipartMixed_PartsAndBoundary(t *testing.T) {
	r := router.New()
	r.Get("/mp", func(c *router.Context) {
		mp := c.MultipartMixed(http.StatusOK)
		defer mp.Close()
		_ = mp.Part(
			textproto.MIMEHeader{"Content-Type": {"application/json"}},
			strings.NewReader(`{"index":0}`),
		)
		_ = mp.Part(
			textproto.MIMEHeader{"Content-Type": {"application/octet-stream"}},
			bytes.NewReader([]byte{0xDE, 0xAD, 0xBE, 0xEF}),
		)
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/mp", nil))

	mediaType, params, err := mime.ParseMediaType(
		rec.Header().Get("Content-Type"),
	)
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	if mediaType != "multipart/mixed" {
		t.Errorf("media type: %q", mediaType)
	}
	if params["boundary"] == "" {
		t.Errorf("boundary missing in %v", params)
	}

	mr := multipart.NewReader(rec.Body, params["boundary"])
	got := []string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		body, _ := io.ReadAll(part)
		got = append(got, part.Header.Get("Content-Type")+":"+string(body))
		_ = part.Close()
	}
	want := []string{
		`application/json:{"index":0}`,
		string(
			[]byte{
				'a',
				'p',
				'p',
				'l',
				'i',
				'c',
				'a',
				't',
				'i',
				'o',
				'n',
				'/',
				'o',
				'c',
				't',
				'e',
				't',
				'-',
				's',
				't',
				'r',
				'e',
				'a',
				'm',
				':',
			},
		) +
			string(
				[]byte{0xDE, 0xAD, 0xBE, 0xEF},
			),
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parts: %q want %q", got, want)
	}
}

func TestStream_FlusherCalledPerSend(t *testing.T) {
	r := router.New()
	r.Get("/s", func(c *router.Context) {
		w := c.Stream(http.StatusOK, "application/jsonl")
		defer w.Close()
		for i := 0; i < 3; i++ {
			_ = w.Send(streamItem{N: i})
		}
	})

	rec := newFlushRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/s", nil))
	if rec.flushes < 3 {
		t.Errorf("flushes: want >=3, got %d", rec.flushes)
	}

	// Sanity: each line should decode.
	br := bufio.NewReader(rec.Body)
	count := 0
	for {
		_, err := br.ReadString('\n')
		if err == io.EOF {
			break
		}
		count++
	}
	if count != 3 {
		t.Errorf("line count: %d", count)
	}
}
