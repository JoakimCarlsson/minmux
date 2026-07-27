package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustNew(t *testing.T, base string, opts ...Option) *Client {
	t.Helper()
	c, err := New(base, opts...)
	if err != nil {
		t.Fatalf("New(%q): %v", base, err)
	}
	return c
}

func TestNew_RejectsNonAbsoluteURLs(t *testing.T) {
	cases := []string{"", "/api", "example.com", "://nope"}
	for _, base := range cases {
		if _, err := New(base); err == nil {
			t.Errorf("New(%q): want an error, got nil", base)
		}
	}
}

func TestNew_AcceptsAbsoluteURLs(t *testing.T) {
	c := mustNew(t, "https://api.example.com/v1")
	if got := c.BaseURL(); got != "https://api.example.com/v1" {
		t.Errorf("BaseURL: got %q", got)
	}
}

func TestNewRequest_AppliesBasePathQueryAndHeaders(t *testing.T) {
	c := mustNew(t, "https://api.example.com/base",
		WithHeader("X-Default", "yes"),
	)
	req, err := c.newRequest(context.Background(), request{
		method:      http.MethodPost,
		path:        "/todos",
		query:       url.Values{"b": {"2"}, "a": {"1"}},
		header:      http.Header{"X-Op": {"op"}},
		body:        strings.NewReader("{}"),
		contentType: "application/json",
		accept:      "application/json",
	})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if got := req.URL.String(); got != "https://api.example.com/base/todos?a=1&b=2" {
		t.Errorf("url: got %q", got)
	}
	if got := req.Header.Get("X-Default"); got != "yes" {
		t.Errorf("client-wide header missing, got %q", got)
	}
	if got := req.Header.Get("X-Op"); got != "op" {
		t.Errorf("per-operation header missing, got %q", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content type: got %q", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("accept: got %q", got)
	}
}

// TestNewRequest_OperationHeaderOverridesClientHeader pins the precedence
// documented on WithHeader: a per-call value replaces the default rather
// than appending a second copy.
func TestNewRequest_OperationHeaderOverridesClientHeader(t *testing.T) {
	c := mustNew(t, "https://api.example.com", WithHeader("X-Trace", "default"))
	req, err := c.newRequest(context.Background(), request{
		method: http.MethodGet,
		path:   "/x",
		header: http.Header{"X-Trace": {"per-call"}},
	})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if got := req.Header.Values(
		"X-Trace",
	); len(got) != 1 ||
		got[0] != "per-call" {
		t.Errorf("X-Trace: got %v, want [per-call]", got)
	}
}

func TestNewRequest_HookErrorAbortsTheCall(t *testing.T) {
	sentinel := errors.New("nope")
	c := mustNew(t, "https://api.example.com",
		WithRequestHook(func(*http.Request) error { return sentinel }),
	)
	_, err := c.newRequest(context.Background(), request{
		method: http.MethodGet, path: "/x",
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("want the hook error, got %v", err)
	}
}

// TestWithTransport_DoesNotMutateSuppliedClient covers the copy-on-wrap
// behavior: handing the same *http.Client to two API clients must not let
// one silently re-route the other.
func TestWithTransport_DoesNotMutateSuppliedClient(t *testing.T) {
	shared := &http.Client{}
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unused")
	})
	c := mustNew(t, "https://api.example.com",
		WithHTTPClient(shared), WithTransport(rt),
	)
	if shared.Transport != nil {
		t.Errorf("supplied http.Client was mutated in place")
	}
	if c.httpClient == shared {
		t.Errorf("client should wrap a copy, not the original")
	}
	if c.httpClient.Transport == nil {
		t.Errorf("transport was not applied to the copy")
	}
}

func TestWithTransport_NilIsIgnored(t *testing.T) {
	c := mustNew(t, "https://api.example.com", WithTransport(nil))
	if c.httpClient == nil {
		t.Fatal("client should survive a nil transport")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestJoinURLPath(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"", "/todos", "/todos"},
		{"/", "/todos", "/todos"},
		{"/api", "/todos", "/api/todos"},
		{"/api/", "/todos", "/api/todos"},
		{"/api", "todos", "/api/todos"},
		{"/api", "", "/api"},
		{"", "", "/"},
	}
	for _, tc := range cases {
		if got := joinURLPath(tc.base, tc.path); got != tc.want {
			t.Errorf("joinURLPath(%q, %q) = %q, want %q",
				tc.base, tc.path, got, tc.want)
		}
	}
}

// TestPathParam_EscapesSeparators is a traversal guard: a path parameter
// containing a slash must not be able to reach a different route.
func TestPathParam_EscapesSeparators(t *testing.T) {
	if got := pathParam("a/b"); got != "a%2Fb" {
		t.Errorf("pathParam(%q) = %q, want a%%2Fb", "a/b", got)
	}
	if got := pathParam("../admin"); strings.Contains(got, "/") {
		t.Errorf("pathParam should escape traversal, got %q", got)
	}
}

// TestWildcardParam_KeepsSegmentSeparators covers the "{name...}" form,
// where slashes are structural and must survive.
func TestWildcardParam_KeepsSegmentSeparators(t *testing.T) {
	if got := wildcardParam("docs/a b/c.txt"); got != "docs/a%20b/c.txt" {
		t.Errorf("wildcardParam = %q", got)
	}
}

func TestFormatValue(t *testing.T) {
	ts := time.Date(2026, 7, 27, 10, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hi", "hi"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"int", 42, "42"},
		{"negative int", -7, "-7"},
		{"int64", int64(9007199254740993), "9007199254740993"},
		{"int32", int32(-5), "-5"},
		{"uint", uint(3), "3"},
		{"uint64", uint64(18446744073709551615), "18446744073709551615"},
		{"float64", 1.5, "1.5"},
		{"float32", float32(2.5), "2.5"},
		{"time", ts, "2026-07-27T10:30:00Z"},
		{"bytes", []byte("raw"), "raw"},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatValue(tc.in); got != tc.want {
				t.Errorf("formatValue(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFormatValue_RoundTripsThroughTheServerBinder checks the property
// that actually matters: whatever the client writes, the minmux binder
// must be able to parse back.
func TestFormatValue_IntegersAreBase10(t *testing.T) {
	if got := formatValue(
		int64(-9223372036854775808),
	); got != "-9223372036854775808" {
		t.Errorf("int64 min: got %q", got)
	}
}

func TestAPIError_MessageAndUnwrap(t *testing.T) {
	e := &APIError{
		Method:     http.MethodGet,
		URL:        "https://api.example.com/todos/1",
		StatusCode: 404,
		Status:     "404 Not Found",
		Body:       []byte(`{"detail":"missing"}`),
	}
	msg := e.Error()
	if !strings.Contains(msg, "GET") ||
		!strings.Contains(msg, "404 Not Found") {
		t.Errorf("message should name the request and status, got %q", msg)
	}
	if !strings.Contains(msg, "missing") {
		t.Errorf("message should quote the body, got %q", msg)
	}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap should be nil when Value is not an error")
	}
}

func TestAPIError_UnwrapsErrorValues(t *testing.T) {
	inner := errors.New("boom")
	e := &APIError{Status: "500", Value: inner}
	if !errors.Is(e, inner) {
		t.Errorf("errors.Is should reach the decoded error value")
	}
	if !strings.Contains(e.Error(), "boom") {
		t.Errorf("message should include the decoded error, got %q", e.Error())
	}
}

func TestAPIError_TruncatesLongBodies(t *testing.T) {
	e := &APIError{Status: "500", Body: bytes.Repeat([]byte("x"), 5000)}
	if len(e.Error()) > errBodyPreview+100 {
		t.Errorf("message should be truncated, got %d chars", len(e.Error()))
	}
	if !strings.HasSuffix(e.Error(), "...") {
		t.Errorf("truncated message should be elided, got %q", e.Error())
	}
	if len(e.Body) != 5000 {
		t.Errorf("Body must retain the full payload, got %d bytes", len(e.Body))
	}
}

type problem struct {
	Detail string `json:"detail"`
}

func TestErrorValue(t *testing.T) {
	want := &problem{Detail: "missing"}
	err := error(&APIError{Status: "404", Value: want})

	got, ok := ErrorValue[*problem](err)
	if !ok || got != want {
		t.Errorf("ErrorValue: got (%v, %v)", got, ok)
	}
	if _, ok := ErrorValue[*APIError](err); ok {
		t.Errorf("ErrorValue should not match a different type")
	}
	if _, ok := ErrorValue[*problem](errors.New("plain")); ok {
		t.Errorf("ErrorValue should reject non-APIError errors")
	}
}

func TestNewAPIError_DecodesDeclaredBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://x.test/todos/1", nil)
	resp := &http.Response{
		StatusCode: 404,
		Status:     "404 Not Found",
		Header:     http.Header{"Content-Type": {"application/problem+json"}},
	}
	body := []byte(`{"detail":"missing"}`)

	var v problem
	e := newAPIError(req, resp, body, &v)
	if e.Value == nil {
		t.Fatalf("declared body should have decoded")
	}
	if v.Detail != "missing" {
		t.Errorf("decoded detail: got %q", v.Detail)
	}
}

// TestNewAPIError_LeavesValueNilOnMismatch is the honesty guarantee: a
// body that does not match the declared shape degrades to raw bytes rather
// than surfacing a misleading zero value.
func TestNewAPIError_LeavesValueNilOnMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://x.test/x", nil)
	cases := map[string]struct {
		contentType string
		body        string
	}{
		"html body":    {"text/html", "<html>oops</html>"},
		"invalid json": {"application/json", "{not json"},
		"empty body":   {"application/json", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: 500,
				Status:     "500 Internal Server Error",
				Header:     http.Header{"Content-Type": {tc.contentType}},
			}
			var v problem
			e := newAPIError(req, resp, []byte(tc.body), &v)
			if e.Value != nil {
				t.Errorf("Value should stay nil, got %+v", e.Value)
			}
			if string(e.Body) != tc.body {
				t.Errorf("raw body should be retained, got %q", e.Body)
			}
		})
	}
}

func TestIsJSONMediaType(t *testing.T) {
	yes := []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/problem+json",
		"APPLICATION/JSON",
	}
	no := []string{"", "text/html", "application/octet-stream", "text/plain"}
	for _, ct := range yes {
		if !isJSONMediaType(ct) {
			t.Errorf("isJSONMediaType(%q) = false, want true", ct)
		}
	}
	for _, ct := range no {
		if isJSONMediaType(ct) {
			t.Errorf("isJSONMediaType(%q) = true, want false", ct)
		}
	}
}

func TestDecodeJSON_EmptyBodyIsNotAnError(t *testing.T) {
	var out problem
	if err := decodeJSON(nil, &out); err != nil {
		t.Errorf("nil body: %v", err)
	}
	if err := decodeJSON([]byte("  \n"), &out); err != nil {
		t.Errorf("blank body: %v", err)
	}
	if err := decodeJSON([]byte("{bad"), &out); err == nil {
		t.Errorf("malformed body should error")
	}
}

func TestReadBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("hello"))}
	got, err := readBody(resp)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q", got)
	}
	if _, err := readBody(&http.Response{}); err != nil {
		t.Errorf("nil body should be tolerated: %v", err)
	}
}

func TestJSONBody_DoesNotEscapeHTML(t *testing.T) {
	r, err := jsonBody(map[string]string{"q": "a&b<c>"})
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	data, _ := io.ReadAll(r)
	if !strings.Contains(string(data), "a&b<c>") {
		t.Errorf("HTML escaping should be off, got %s", data)
	}
}

// TestMultipartBody_IsDeterministic matters because a nondeterministic
// body would make request fixtures and recorded cassettes flaky.
func TestMultipartBody_IsDeterministic(t *testing.T) {
	build := func() string {
		fields := url.Values{"z": {"26"}, "a": {"1"}, "m": {"13"}}
		files := map[string][]FormFile{
			"second": {{Filename: "b.txt", Content: strings.NewReader("B")}},
			"first": {{
				Filename:    "a.txt",
				ContentType: "text/plain",
				Content:     strings.NewReader("A"),
			}},
		}
		body, _, err := multipartBody(fields, files)
		if err != nil {
			t.Fatalf("multipartBody: %v", err)
		}
		data, _ := io.ReadAll(body)
		return stripBoundary(string(data))
	}
	first := build()
	for i := range 10 {
		if got := build(); got != first {
			t.Fatalf("run %d produced different part ordering", i)
		}
	}
	if strings.Index(first, `name="a"`) > strings.Index(first, `name="m"`) {
		t.Errorf("fields should be written in sorted order:\n%s", first)
	}
	if strings.Index(
		first,
		`name="first"`,
	) > strings.Index(
		first,
		`name="second"`,
	) {
		t.Errorf("files should be written in sorted order:\n%s", first)
	}
}

func stripBoundary(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\r\n") {
		if strings.HasPrefix(line, "--") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestMultipartBody_RoundTripsThroughStdlibReader(t *testing.T) {
	files := map[string][]FormFile{
		"doc": {{
			Filename:    "note.txt",
			ContentType: "text/plain",
			Content:     strings.NewReader("hello"),
		}},
	}
	body, ct, err := multipartBody(url.Values{"note": {"hi"}}, files)
	if err != nil {
		t.Fatalf("multipartBody: %v", err)
	}
	boundary, err := multipartBoundary(ct)
	if err != nil {
		t.Fatalf("multipartBoundary: %v", err)
	}
	mr := multipart.NewReader(body, boundary)
	form, err := mr.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	if got := form.Value["note"]; len(got) != 1 || got[0] != "hi" {
		t.Errorf("note field: got %v", got)
	}
	fh := form.File["doc"]
	if len(fh) != 1 || fh[0].Filename != "note.txt" {
		t.Fatalf("doc file: got %+v", fh)
	}
	f, _ := fh[0].Open()
	content, _ := io.ReadAll(f)
	if string(content) != "hello" {
		t.Errorf("file content: got %q", content)
	}
}

type item struct {
	N int `json:"n"`
}

func streamResponse(ct, body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {ct}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func collect(t *testing.T, seq func(func(item, error) bool)) []int {
	t.Helper()
	var out []int
	for v, err := range seq {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, v.N)
	}
	return out
}

func TestJSONStreamSeq_LineFramed(t *testing.T) {
	for _, ct := range []string{"application/jsonl", "application/x-ndjson"} {
		resp := streamResponse(ct, "{\"n\":1}\n\n{\"n\":2}\n")
		got := collect(t, jsonStreamSeq[item](resp))
		if len(got) != 2 || got[0] != 1 || got[1] != 2 {
			t.Errorf("%s: got %v, want [1 2]", ct, got)
		}
	}
}

func TestJSONStreamSeq_RecordSeparatorFramed(t *testing.T) {
	body := "\x1e{\"n\":1}\n\x1e{\"n\":2}\n"
	resp := streamResponse("application/json-seq", body)
	got := collect(t, jsonStreamSeq[item](resp))
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("got %v, want [1 2]", got)
	}
}

// TestJSONStreamSeq_ClosesBodyOnEarlyBreak guards the leak the doc comment
// promises not to cause.
func TestJSONStreamSeq_ClosesBodyOnEarlyBreak(t *testing.T) {
	tracker := &closeTracker{
		Reader: strings.NewReader("{\"n\":1}\n{\"n\":2}\n"),
	}
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"application/jsonl"}},
		Body:   tracker,
	}
	for range jsonStreamSeq[item](resp) {
		break
	}
	if !tracker.closed {
		t.Errorf("body should be closed when iteration stops early")
	}
}

func TestJSONStreamSeq_YieldsDecodeErrorsAndContinues(t *testing.T) {
	resp := streamResponse("application/jsonl", "{\"n\":1}\n{bad}\n{\"n\":3}\n")
	var values []int
	var errCount int
	for v, err := range jsonStreamSeq[item](resp) {
		if err != nil {
			errCount++
			continue
		}
		values = append(values, v.N)
	}
	if errCount != 1 {
		t.Errorf("want 1 decode error, got %d", errCount)
	}
	if len(values) != 2 || values[0] != 1 || values[1] != 3 {
		t.Errorf("a bad record should not end the stream, got %v", values)
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestSSESeq(t *testing.T) {
	body := "id: 1\nevent: tick\ndata: {\"n\":1}\nretry: 500\n\n" +
		": a comment\n" +
		"data: line one\ndata: line two\n\n"
	var events []SSEEvent
	for ev, err := range sseSeq(io.NopCloser(strings.NewReader(body))) {
		if err != nil {
			t.Fatalf("sse error: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(events), events)
	}
	if events[0].ID != "1" || events[0].Event != "tick" ||
		events[0].Retry != 500 {
		t.Errorf("first event fields: %+v", events[0])
	}
	if string(events[0].Data) != `{"n":1}` {
		t.Errorf("first event data: %q", events[0].Data)
	}
	if string(events[1].Data) != "line one\nline two" {
		t.Errorf("multi-line data should be joined, got %q", events[1].Data)
	}
}

func TestSSEData(t *testing.T) {
	got, err := SSEData[item](SSEEvent{Data: []byte(`{"n":7}`)})
	if err != nil {
		t.Fatalf("SSEData: %v", err)
	}
	if got.N != 7 {
		t.Errorf("got %+v", got)
	}
	if _, err := SSEData[item](SSEEvent{Data: []byte("nope")}); err == nil {
		t.Errorf("malformed data should error")
	}
}

func TestMultipartSeq(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, s := range []string{"one", "two"} {
		pw, err := w.CreateFormField(s)
		if err != nil {
			t.Fatalf("CreateFormField: %v", err)
		}
		if _, err := pw.Write([]byte(s)); err != nil {
			t.Fatalf("writing part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	resp := &http.Response{
		Header: http.Header{"Content-Type": {
			"multipart/mixed; boundary=" + w.Boundary(),
		}},
		Body: io.NopCloser(&buf),
	}
	var bodies []string
	for part, err := range multipartSeq(resp) {
		if err != nil {
			t.Fatalf("part error: %v", err)
		}
		data, _ := io.ReadAll(part.Body)
		bodies = append(bodies, string(data))
	}
	if len(bodies) != 2 || bodies[0] != "one" || bodies[1] != "two" {
		t.Errorf("got %v, want [one two]", bodies)
	}
}

func TestMultipartSeq_ReportsMissingBoundary(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"multipart/mixed"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}
	var got error
	for _, err := range multipartSeq(resp) {
		got = err
	}
	if got == nil || !strings.Contains(got.Error(), "boundary") {
		t.Errorf("want a boundary error, got %v", got)
	}
}

func TestJSONLinesBody(t *testing.T) {
	seq := func(yield func(item, error) bool) {
		for i := 1; i <= 3; i++ {
			if !yield(item{N: i}, nil) {
				return
			}
		}
	}
	data, err := io.ReadAll(jsonLinesBody(seq, framerLines))
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	want := "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

func TestJSONLinesBody_RecordSeparatorFraming(t *testing.T) {
	seq := func(yield func(item, error) bool) { yield(item{N: 1}, nil) }
	data, err := io.ReadAll(jsonLinesBody(seq, framerRS))
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !bytes.HasPrefix(data, []byte{recordSeparator}) {
		t.Errorf("json-seq records must start with 0x1E, got %q", data)
	}
}

// TestJSONLinesBody_PropagatesSequenceErrors ensures a producer failure
// aborts the request body instead of silently truncating it.
func TestJSONLinesBody_PropagatesSequenceErrors(t *testing.T) {
	sentinel := errors.New("producer failed")
	seq := func(yield func(item, error) bool) {
		if !yield(item{N: 1}, nil) {
			return
		}
		yield(item{}, sentinel)
	}
	_, err := io.ReadAll(jsonLinesBody(seq, framerLines))
	if !errors.Is(err, sentinel) {
		t.Errorf("want the producer error, got %v", err)
	}
}

func TestFramerForMediaType(t *testing.T) {
	rs := []string{"application/json-seq", "application/geo+json-seq"}
	lines := []string{"application/jsonl", "application/x-ndjson", "text/plain"}
	for _, ct := range rs {
		if framerForMediaType(ct) != framerRS {
			t.Errorf("%s should be record-separated", ct)
		}
	}
	for _, ct := range lines {
		if framerForMediaType(ct) != framerLines {
			t.Errorf("%s should be line-framed", ct)
		}
	}
}

func TestStreamError_ConsumesBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://x.test/stream", nil)
	tracker := &closeTracker{Reader: strings.NewReader(`{"detail":"nope"}`)}
	resp := &http.Response{
		StatusCode: 503,
		Status:     "503 Service Unavailable",
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       tracker,
	}
	var v problem
	e := streamError(req, resp, &v)
	if !tracker.closed {
		t.Errorf("body should be drained and closed so the conn can be reused")
	}
	if e.StatusCode != 503 {
		t.Errorf("status: got %d", e.StatusCode)
	}
	if v.Detail != "nope" {
		t.Errorf("error body should decode, got %+v", v)
	}
}

func TestClientDo_WrapsTransportErrors(t *testing.T) {
	c := mustNew(
		t,
		"https://api.example.com",
		WithTransport(
			roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed")
			}),
		),
	)
	req, err := c.newRequest(context.Background(), request{
		method: http.MethodGet, path: "/x",
	})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if _, err := c.do(
		req,
	); err == nil ||
		!strings.Contains(err.Error(), "dial failed") {
		t.Errorf("want the wrapped transport error, got %v", err)
	}
}

func TestClientDo_HonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter, r *http.Request,
	) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := mustNew(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := c.newRequest(ctx, request{method: http.MethodGet, path: "/x"})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if _, err := c.do(req); !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := truncate("abcdefghij", 3); got != "abc..." {
		t.Errorf("got %q", got)
	}
}
