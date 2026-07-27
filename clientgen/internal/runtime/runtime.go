package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// recordSeparator is the RFC 7464 prefix byte framing application/json-seq
// records.
const recordSeparator = 0x1E

// errBodyPreview caps how much of an unexpected response body is quoted
// into an APIError message. The full payload stays on APIError.Body.
const errBodyPreview = 256

// Client is a typed client for the API. Construct it with New and share a
// single instance across goroutines: it holds no per-call state, so it is
// safe for concurrent use.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	header     http.Header
	hooks      []func(*http.Request) error
}

// Option customizes a Client at construction time. Options apply in the
// order given, so a later option overrides an earlier one.
type Option func(*Client)

// WithHTTPClient replaces the *http.Client used for every request,
// handing over control of timeouts, redirect policy, and cookie handling.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithTransport wraps the current *http.Client with a custom
// http.RoundTripper, the seam for auth, retries, and tracing. The
// underlying client is copied rather than mutated, so a client passed to
// WithHTTPClient is never modified in place.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) {
		if rt == nil {
			return
		}
		next := *c.httpClient
		next.Transport = rt
		c.httpClient = &next
	}
}

// WithHeader sets a header sent on every request. Per-operation header
// parameters are applied afterwards and win on conflict.
func WithHeader(name, value string) Option {
	return func(c *Client) { c.header.Set(name, value) }
}

// WithRequestHook registers a callback run against every request after it
// is built but before it is sent. Returning an error aborts the call. Use
// it for dynamic per-request concerns such as signing; prefer
// WithTransport for anything that needs to observe the response too.
func WithRequestHook(fn func(*http.Request) error) Option {
	return func(c *Client) {
		if fn != nil {
			c.hooks = append(c.hooks, fn)
		}
	}
}

// New constructs a Client against an absolute base URL such as
// "https://api.example.com" or "http://127.0.0.1:8080/api". Any path on
// the base URL prefixes every operation path.
func New(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("client: parsing base URL %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("client: base URL %q must be absolute", baseURL)
	}
	c := &Client{
		baseURL:    u,
		httpClient: &http.Client{},
		header:     http.Header{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the base URL the client was constructed with.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// APIError is returned for any response the operation does not declare as
// its success status.
//
// Value holds the response body decoded into the Go type the spec declares
// for that status code, or nil when the status is undeclared or the body
// did not decode. Body always carries the raw bytes, so nothing is lost
// when Value is nil. Recover the typed value with ErrorValue.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
	Value      any
}

// Error implements the error interface.
func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s: %s", e.Method, e.URL, e.Status)
	if v, ok := e.Value.(error); ok {
		return msg + ": " + v.Error()
	}
	if body := strings.TrimSpace(string(e.Body)); body != "" {
		return msg + ": " + truncate(body, errBodyPreview)
	}
	return msg
}

// Unwrap exposes the decoded error body when it happens to implement
// error, so errors.Is and errors.As reach through to it.
func (e *APIError) Unwrap() error {
	if v, ok := e.Value.(error); ok {
		return v
	}
	return nil
}

// ErrorValue extracts the decoded error body from err as type T. It
// reports false when err is not an *APIError, or when the status carried
// no declared body of that type.
//
//	if pd, ok := ErrorValue[*ProblemDetails](err); ok {
//	    log.Println(pd.Detail)
//	}
func ErrorValue[T any](err error) (T, bool) {
	var zero T
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return zero, false
	}
	v, ok := apiErr.Value.(T)
	return v, ok
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// request is the assembled description of one call, built by generated
// operation methods and handed to newRequest.
type request struct {
	method      string
	path        string
	query       url.Values
	header      http.Header
	body        io.Reader
	contentType string
	accept      string
}

func (c *Client) newRequest(
	ctx context.Context,
	r request,
) (*http.Request, error) {
	u := *c.baseURL
	u.Path = joinURLPath(u.Path, r.path)
	u.RawPath = ""
	if len(r.query) > 0 {
		u.RawQuery = r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u.String(), r.body)
	if err != nil {
		return nil, fmt.Errorf("client: building request: %w", err)
	}
	for name, values := range c.header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	for name, values := range r.header {
		req.Header.Del(name)
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	if r.accept != "" {
		req.Header.Set("Accept", r.accept)
	}
	for _, hook := range c.hooks {
		if err := hook(req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL, err)
	}
	return resp, nil
}

// joinURLPath concatenates a base path and an operation path with exactly
// one separating slash.
func joinURLPath(base, p string) string {
	base = strings.TrimSuffix(base, "/")
	if p == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// pathParam escapes a single path segment.
func pathParam(v string) string { return url.PathEscape(v) }

// wildcardParam escapes a multi-segment "{name...}" path value, preserving
// the slashes that separate its segments.
func wildcardParam(v string) string {
	parts := strings.Split(v, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// formatValue renders a parameter value the way the minmux server binder
// parses it back: RFC 3339 for timestamps, base-10 for numbers, "true" /
// "false" for booleans.
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	case []byte:
		return string(t)
	case int:
		return strconv.Itoa(t)
	case int8, int16, int32, int64:
		return strconv.FormatInt(reflectInt(t), 10)
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflectUint(t), 10)
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case fmt.Stringer:
		return t.String()
	}
	return fmt.Sprint(v)
}

func reflectInt(v any) int64 {
	switch t := v.(type) {
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	}
	return 0
}

func reflectUint(v any) uint64 {
	switch t := v.(type) {
	case uint:
		return uint64(t)
	case uint8:
		return uint64(t)
	case uint16:
		return uint64(t)
	case uint32:
		return uint64(t)
	case uint64:
		return t
	}
	return 0
}

// readBody consumes and closes a response body. A read failure surfaces as
// an error rather than a silently short body.
func readBody(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		return nil, nil
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return b, fmt.Errorf("client: reading response body: %w", err)
	}
	return b, nil
}

// decodeJSON unmarshals a response body, treating an empty body as a
// no-op so that a declared type on a bodiless response is not an error.
func decodeJSON(body []byte, out any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("client: decoding response body: %w", err)
	}
	return nil
}

// newAPIError builds the error returned for an undeclared or non-success
// status. value, when non-nil, is a pointer to the Go type the spec
// declares for this status; it is attached only if the body actually
// decodes into it, so a mismatched body degrades to raw bytes instead of a
// misleading zero value.
func newAPIError(
	req *http.Request,
	resp *http.Response,
	body []byte,
	value any,
) *APIError {
	e := &APIError{
		Method:     req.Method,
		URL:        req.URL.String(),
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       body,
	}
	if value == nil || len(bytes.TrimSpace(body)) == 0 {
		return e
	}
	if !isJSONMediaType(resp.Header.Get("Content-Type")) {
		return e
	}
	if err := json.Unmarshal(body, value); err == nil {
		e.Value = value
	}
	return e
}

// isJSONMediaType reports whether a Content-Type carries JSON, accepting
// the +json structured suffix (application/problem+json and friends).
func isJSONMediaType(ct string) bool {
	mt := normalizeMediaType(ct)
	if mt == "" {
		return false
	}
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func normalizeMediaType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// jsonBody encodes a value as a JSON request body.
func jsonBody(v any) (io.Reader, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("client: encoding request body: %w", err)
	}
	return &buf, nil
}

// FormFile is one file part of a multipart/form-data request body.
// ContentType is optional; when empty the part carries no explicit
// Content-Type header.
type FormFile struct {
	Filename    string
	ContentType string
	Content     io.Reader
}

// multipartBody assembles a multipart/form-data request body and returns
// it alongside the Content-Type header carrying the generated boundary.
// Fields and files are written in sorted key order so the same inputs
// always produce byte-identical output.
func multipartBody(
	fields url.Values,
	files map[string][]FormFile,
) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		for _, v := range fields[name] {
			if err := w.WriteField(name, v); err != nil {
				return nil, "", fmt.Errorf(
					"client: writing form field %q: %w",
					name,
					err,
				)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		for _, f := range files[name] {
			if err := writeFilePart(w, name, f); err != nil {
				return nil, "", err
			}
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("client: closing multipart body: %w", err)
	}
	return &buf, w.FormDataContentType(), nil
}

func writeFilePart(w *multipart.Writer, name string, f FormFile) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(
		`form-data; name=%s; filename=%s`,
		strconv.Quote(name), strconv.Quote(f.Filename),
	))
	if f.ContentType != "" {
		h.Set("Content-Type", f.ContentType)
	}
	part, err := w.CreatePart(h)
	if err != nil {
		return fmt.Errorf("client: creating file part %q: %w", name, err)
	}
	if f.Content == nil {
		return nil
	}
	if _, err := io.Copy(part, f.Content); err != nil {
		return fmt.Errorf("client: writing file part %q: %w", name, err)
	}
	return nil
}

// streamFramer selects the record framing of a sequential JSON media type.
type streamFramer int

const (
	framerLines streamFramer = iota
	framerRS
)

func framerForMediaType(ct string) streamFramer {
	switch normalizeMediaType(ct) {
	case "application/json-seq", "application/geo+json-seq":
		return framerRS
	}
	return framerLines
}

// jsonLinesBody streams a sequence as a sequential-JSON request body. The
// encoding happens on a background goroutine feeding an io.Pipe, so an
// arbitrarily long sequence never has to be buffered in memory. An error
// yielded by the sequence aborts the request body with that error.
func jsonLinesBody[T any](
	seq iter.Seq2[T, error],
	framer streamFramer,
) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		enc := json.NewEncoder(pw)
		enc.SetEscapeHTML(false)
		for v, err := range seq {
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if framer == framerRS {
				if _, werr := pw.Write([]byte{recordSeparator}); werr != nil {
					_ = pw.CloseWithError(werr)
					return
				}
			}
			if werr := enc.Encode(v); werr != nil {
				_ = pw.CloseWithError(werr)
				return
			}
		}
		_ = pw.Close()
	}()
	return pr
}

// jsonStreamSeq decodes a sequential-JSON response body, picking the
// framer from the response Content-Type so one operation can serve
// jsonl, ndjson, and json-seq alike.
//
// The returned sequence owns the response body and closes it when
// iteration ends, including on an early break. A caller that never
// iterates must close resp.Body itself.
func jsonStreamSeq[T any](resp *http.Response) iter.Seq2[T, error] {
	if framerForMediaType(resp.Header.Get("Content-Type")) == framerRS {
		return jsonSeqSeq[T](resp.Body)
	}
	return jsonLineSeq[T](resp.Body)
}

func jsonLineSeq[T any](rc io.ReadCloser) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		defer func() { _ = rc.Close() }()
		var zero T
		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 64*1024), 1<<24)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var v T
			if err := json.Unmarshal(line, &v); err != nil {
				if !yield(zero, err) {
					return
				}
				continue
			}
			if !yield(v, nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(zero, err)
		}
	}
}

func jsonSeqSeq[T any](rc io.ReadCloser) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		defer func() { _ = rc.Close() }()
		var zero T
		br := bufio.NewReader(rc)
		for {
			b, err := br.ReadByte()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(zero, err)
				return
			}
			if b != recordSeparator {
				continue
			}
			record, err := readJSONSeqRecord(br)
			if err != nil {
				yield(zero, err)
				return
			}
			if len(bytes.TrimSpace(record)) == 0 {
				continue
			}
			var v T
			if err := json.Unmarshal(record, &v); err != nil {
				if !yield(zero, err) {
					return
				}
				continue
			}
			if !yield(v, nil) {
				return
			}
		}
	}
}

// readJSONSeqRecord reads up to the next record separator, leaving that
// separator in the stream for the next read to consume.
func readJSONSeqRecord(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		b, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if b == recordSeparator {
			_ = br.UnreadByte()
			return buf.Bytes(), nil
		}
		buf.WriteByte(b)
	}
}

// SSEEvent is one decoded frame of a text/event-stream response. Data
// holds the raw payload bytes so nothing is lost for non-JSON streams;
// use SSEData to decode a JSON payload into a typed value.
type SSEEvent struct {
	Event string
	Data  []byte
	ID    string
	Retry int
}

// SSEData decodes an event's JSON payload into T.
func SSEData[T any](ev SSEEvent) (T, error) {
	var out T
	if err := json.Unmarshal(ev.Data, &out); err != nil {
		return out, fmt.Errorf("client: decoding SSE data: %w", err)
	}
	return out, nil
}

// sseSeq decodes a text/event-stream response body per the WHATWG parsing
// rules: fields accumulate until a blank line dispatches the event, and
// comment lines are skipped. The sequence owns the body and closes it when
// iteration ends.
func sseSeq(rc io.ReadCloser) iter.Seq2[SSEEvent, error] {
	return func(yield func(SSEEvent, error) bool) {
		defer func() { _ = rc.Close() }()
		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 64*1024), 1<<24)
		var (
			ev     SSEEvent
			data   bytes.Buffer
			haveEv bool
		)
		flush := func() bool {
			if !haveEv {
				return true
			}
			ev.Data = []byte(strings.TrimSuffix(data.String(), "\n"))
			ok := yield(ev, nil)
			ev = SSEEvent{}
			data.Reset()
			haveEv = false
			return ok
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			haveEv = true
			name, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch name {
			case "event":
				ev.Event = value
			case "data":
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(value)
			case "id":
				ev.ID = value
			case "retry":
				if n, err := strconv.Atoi(value); err == nil {
					ev.Retry = n
				}
			}
		}
		if err := scanner.Err(); err != nil {
			yield(SSEEvent{}, err)
			return
		}
		flush()
	}
}

// Part is one part of a multipart/mixed response. Body is owned by the
// underlying reader and is only valid until the next iteration step.
type Part struct {
	Header textproto.MIMEHeader
	Body   io.Reader
}

// multipartSeq iterates the parts of a multipart response, taking the
// boundary from the response Content-Type. The sequence owns the body and
// closes it when iteration ends.
func multipartSeq(resp *http.Response) iter.Seq2[Part, error] {
	return func(yield func(Part, error) bool) {
		defer func() { _ = resp.Body.Close() }()
		boundary, err := multipartBoundary(resp.Header.Get("Content-Type"))
		if err != nil {
			yield(Part{}, err)
			return
		}
		mr := multipart.NewReader(resp.Body, boundary)
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(Part{}, err)
				return
			}
			ok := yield(Part{Header: p.Header, Body: p}, nil)
			_ = p.Close()
			if !ok {
				return
			}
		}
	}
}

func multipartBoundary(ct string) (string, error) {
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", fmt.Errorf("client: parsing Content-Type %q: %w", ct, err)
	}
	b := params["boundary"]
	if b == "" {
		return "", fmt.Errorf("client: Content-Type %q has no boundary", ct)
	}
	return b, nil
}

// streamError converts a non-success status on a streaming operation into
// an APIError, consuming the body so the connection can be reused.
func streamError(
	req *http.Request,
	resp *http.Response,
	value any,
) *APIError {
	body, _ := readBody(resp)
	return newAPIError(req, resp, body, value)
}
