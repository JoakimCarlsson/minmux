package router_test

import (
	"bytes"
	"io"
	"iter"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type ingestRecord struct {
	N int    `json:"n"`
	S string `json:"s"`
}

type jsonlParams struct {
	Body iter.Seq2[ingestRecord, error] `body:"" contentType:"application/jsonl, application/x-ndjson"`
}

type jsonSeqParams struct {
	Body iter.Seq2[ingestRecord, error] `body:"" contentType:"application/json-seq"`
}

type sseInputParams struct {
	Events iter.Seq2[router.SSEEvent, error] `body:"" contentType:"text/event-stream"`
}

type multipartInputParams struct {
	Parts iter.Seq2[router.Part, error] `body:"" contentType:"multipart/mixed"`
}

func TestBinder_JSONLIterator(t *testing.T) {
	r := router.New()
	var got []ingestRecord
	var iterErr error
	r.Post("/ingest", func(c *router.Context, p jsonlParams) {
		for v, err := range p.Body {
			if err != nil {
				iterErr = err
				continue
			}
			got = append(got, v)
		}
		c.NoContent()
	})

	body := strings.Join([]string{
		`{"n":1,"s":"a"}`,
		`{"n":2,"s":"b"}`,
		``,
		`{"n":3,"s":"c"}`,
	}, "\n")
	req := httptest.NewRequest("POST", "/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/jsonl")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if iterErr != nil {
		t.Fatalf("iter error: %v", iterErr)
	}
	if len(got) != 3 || got[0].S != "a" || got[2].N != 3 {
		t.Errorf("records: %+v", got)
	}
}

func TestBinder_JSONLDecodeErrorYieldsError(t *testing.T) {
	r := router.New()
	var got []ingestRecord
	var errs []error
	r.Post("/ingest", func(c *router.Context, p jsonlParams) {
		for v, err := range p.Body {
			if err != nil {
				errs = append(errs, err)
				continue
			}
			got = append(got, v)
		}
		c.NoContent()
	})

	body := strings.Join([]string{
		`{"n":1,"s":"a"}`,
		`not-json`,
		`{"n":2,"s":"b"}`,
	}, "\n")
	req := httptest.NewRequest("POST", "/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-ndjson")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if len(errs) != 1 {
		t.Fatalf("expected one decode error, got %v", errs)
	}
	if len(got) != 2 || got[0].N != 1 || got[1].N != 2 {
		t.Errorf("decoded records: %+v", got)
	}
}

func TestBinder_JSONSeqIterator(t *testing.T) {
	r := router.New()
	var got []ingestRecord
	r.Post("/ingest", func(c *router.Context, p jsonSeqParams) {
		for v, err := range p.Body {
			if err != nil {
				t.Errorf("iter error: %v", err)
				continue
			}
			got = append(got, v)
		}
		c.NoContent()
	})

	body := "\x1e" + `{"n":1,"s":"a"}` + "\n" +
		"\x1e" + `{"n":2,"s":"b"}` + "\n"
	req := httptest.NewRequest("POST", "/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-seq")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if len(got) != 2 || got[0].N != 1 || got[1].S != "b" {
		t.Errorf("records: %+v", got)
	}
}

func TestBinder_SSEIterator(t *testing.T) {
	r := router.New()
	var got []router.SSEEvent
	r.Post("/sse", func(c *router.Context, p sseInputParams) {
		for ev, err := range p.Events {
			if err != nil {
				t.Errorf("iter error: %v", err)
				continue
			}
			got = append(got, ev)
		}
		c.NoContent()
	})

	body := strings.Join([]string{
		"event: addString",
		"data: This data is formatted",
		"data: across two lines",
		"retry: 5",
		"",
		"event: addInt64",
		"data: 1234.5678",
		"unknownField: ignored",
		"",
		": this is a comment",
		"event: addJSON",
		`data: {"foo":42}`,
		"",
	}, "\n")
	req := httptest.NewRequest("POST", "/sse", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/event-stream")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if len(got) != 3 {
		t.Fatalf("events: want 3, got %d (%+v)", len(got), got)
	}
	if got[0].Event != "addString" {
		t.Errorf("ev[0].Event: %q", got[0].Event)
	}
	if got[0].Data != "This data is formatted\nacross two lines" {
		t.Errorf("ev[0].Data: %q", got[0].Data)
	}
	if got[0].Retry != 5 {
		t.Errorf("ev[0].Retry: %d", got[0].Retry)
	}
	if got[1].Event != "addInt64" || got[1].Data != "1234.5678" {
		t.Errorf("ev[1]: %+v", got[1])
	}
	if got[2].Event != "addJSON" || got[2].Data != `{"foo":42}` {
		t.Errorf("ev[2]: %+v", got[2])
	}
}

func TestBinder_MultipartMixedIterator(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	writeMixedPart(t, mw, "application/json", []byte(`{"index":0}`))
	writeMixedPart(t, mw, "application/octet-stream", []byte{1, 2, 3})
	_ = mw.Close()

	r := router.New()
	var got []string
	r.Post("/mp", func(c *router.Context, p multipartInputParams) {
		for part, err := range p.Parts {
			if err != nil {
				t.Errorf("iter error: %v", err)
				continue
			}
			data, _ := io.ReadAll(part.Body)
			got = append(got, part.Header.Get("Content-Type")+":"+string(data))
		}
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/mp", &body)
	req.Header.Set("Content-Type",
		"multipart/mixed; boundary="+mw.Boundary())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if len(got) != 2 {
		t.Fatalf("parts: %v", got)
	}
	if got[0] != `application/json:{"index":0}` {
		t.Errorf("part 0: %q", got[0])
	}
	if got[1] != string(append([]byte("application/octet-stream:"),
		1, 2, 3)) {
		t.Errorf("part 1 bytes: %q", got[1])
	}
}

func TestBinder_StreamWrongContentTypeRejected(t *testing.T) {
	r := router.New()
	r.Post("/ingest", func(c *router.Context, p jsonlParams) {
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/ingest", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
}

type misuseSSEParams struct {
	Wrong iter.Seq2[ingestRecord, error] `body:"" contentType:"text/event-stream"`
}

func TestBinder_SSEItemTypeMismatchPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SSE iter has non-SSEEvent item type")
		}
	}()
	r := router.New()
	r.Post("/sse", func(c *router.Context, p misuseSSEParams) {})
}

type misuseMixedYieldTypesParams struct {
	Wrong iter.Seq2[ingestRecord, error] `body:"" contentType:"application/jsonl, text/event-stream"`
}

func TestBinder_MixedYieldTypesPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal(
				"expected panic when content types yield different item types",
			)
		}
	}()
	r := router.New()
	r.Post("/x", func(c *router.Context, p misuseMixedYieldTypesParams) {})
}

type jsonAllFramingsParams struct {
	Body iter.Seq2[ingestRecord, error] `body:"" contentType:"application/jsonl, application/x-ndjson, application/json-seq, application/geo+json-seq"`
}

func TestBinder_AllSequentialJSONFramingsCoexist(t *testing.T) {
	r := router.New()
	got := map[string][]int{}
	r.Post("/x", func(c *router.Context, p jsonAllFramingsParams) {
		ct := c.Request.Header.Get("Content-Type")
		for v, err := range p.Body {
			if err != nil {
				t.Errorf("%s iter error: %v", ct, err)
				continue
			}
			got[ct] = append(got[ct], v.N)
		}
		c.NoContent()
	})

	cases := []struct {
		ct, body string
		want     []int
	}{
		{
			"application/jsonl",
			`{"n":1,"s":"a"}` + "\n" + `{"n":2,"s":"b"}`,
			[]int{1, 2},
		},
		{
			"application/x-ndjson",
			`{"n":3,"s":"c"}` + "\n" + `{"n":4,"s":"d"}`,
			[]int{3, 4},
		},
		{
			"application/json-seq",
			"\x1e" + `{"n":5,"s":"e"}` + "\n" +
				"\x1e" + `{"n":6,"s":"f"}` + "\n",
			[]int{5, 6},
		},
		{
			"application/geo+json-seq",
			"\x1e" + `{"n":7,"s":"g"}` + "\n",
			[]int{7},
		},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/x", strings.NewReader(c.body))
		req.Header.Set("Content-Type", c.ct)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: status %d body=%s", c.ct, rec.Code, rec.Body)
			continue
		}
		if len(got[c.ct]) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.ct, got[c.ct], c.want)
			continue
		}
		for i, v := range c.want {
			if got[c.ct][i] != v {
				t.Errorf("%s[%d]: got %d, want %d",
					c.ct, i, got[c.ct][i], v)
			}
		}
	}
}

type misuseNoContentTypeParams struct {
	Body iter.Seq2[ingestRecord, error] `body:""`
}

func TestBinder_IterBodyRequiresContentType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when iter.Seq2 body has no contentType tag")
		}
	}()
	r := router.New()
	r.Post("/x", func(c *router.Context, p misuseNoContentTypeParams) {})
}

func writeMixedPart(
	t *testing.T,
	mw *multipart.Writer,
	contentType string,
	payload []byte,
) {
	t.Helper()
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", contentType)
	w, err := mw.CreatePart(h)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write part: %v", err)
	}
}
