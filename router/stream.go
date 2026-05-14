package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
)

// ErrStreamClosed is returned by stream writer methods after Close. Once a
// writer is closed it cannot be reused; create a new one for the next
// response.
var ErrStreamClosed = errors.New("minmux: stream writer closed")

// recordSeparator is the prefix byte for application/json-seq records per
// RFC 7464.
const recordSeparator = 0x1E

// SSEEvent is one frame of a text/event-stream response or request body.
// Data may be any Go value: strings are sent verbatim (split on newlines
// into multiple data: lines), other types are JSON-encoded via the router
// codec.
type SSEEvent struct {
	// Event names the event type (the "event:" field). Empty omits it.
	Event string
	// Data carries the event payload. Strings (and []byte) are sent
	// verbatim; any other value is JSON-encoded.
	Data any
	// ID sets the event id ("id:" field). Empty omits it.
	ID string
	// Retry is the reconnect time in milliseconds ("retry:" field). Zero
	// omits it.
	Retry int
	// Comment is sent as a single SSE comment line (": comment").
	// Useful as a keep-alive. Newlines are stripped.
	Comment string
}

// Part is one item read from a multipart/mixed request body (or written
// via MultipartWriter.Part). Body is owned by the multipart reader and is
// only valid until the next iteration / write.
type Part struct {
	Header textproto.MIMEHeader
	Body   io.Reader
}

// StreamWriter writes a sequential media-type response one frame at a time.
//
// The writer is created via *Context.Stream and bound to a single Content-Type
// (application/jsonl, application/x-ndjson, application/json-seq, or any
// other line-/RS-framed sequential type). Send encodes one item using the
// router codec, writes the framing bytes, and flushes if the underlying
// ResponseWriter supports http.Flusher.
//
// Concurrent Send calls are not safe; coordinate at the producer side.
type StreamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	codec   Codec
	ctx     context.Context
	status  int
	ct      string
	framer  streamFramer
	started bool
	closed  bool
	buf     bytes.Buffer
}

type streamFramer int

const (
	framerLines streamFramer = iota // jsonl, ndjson: payload + "\n"
	framerRS                        // json-seq: 0x1E + payload + "\n"
)

func newStreamWriter(c *Context, status int, contentType string) *StreamWriter {
	flusher, _ := c.Writer.(http.Flusher)
	return &StreamWriter{
		w:       c.Writer,
		flusher: flusher,
		codec:   c.codec,
		ctx:     c.Request.Context(),
		status:  status,
		ct:      contentType,
		framer:  framerForContentType(contentType),
	}
}

func framerForContentType(ct string) streamFramer {
	switch normalizeMediaType(ct) {
	case "application/json-seq", "application/geo+json-seq":
		return framerRS
	}
	return framerLines
}

// Send encodes v with the router codec and writes one framed record. The
// first call also writes the response headers (Content-Type, Cache-Control:
// no-cache) and the status code. Send returns ErrStreamClosed after Close
// and context.Canceled / context.DeadlineExceeded if the client disconnects.
func (s *StreamWriter) Send(v any) error {
	if s.closed {
		return ErrStreamClosed
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.ensureStarted()
	s.buf.Reset()
	if s.framer == framerRS {
		s.buf.WriteByte(recordSeparator)
	}
	if err := s.codec.Encode(&s.buf, v); err != nil {
		return err
	}
	if b := s.buf.Bytes(); len(b) == 0 || b[len(b)-1] != '\n' {
		s.buf.WriteByte('\n')
	}
	if _, err := s.w.Write(s.buf.Bytes()); err != nil {
		return err
	}
	s.flush()
	return nil
}

// Flush forces a flush of any buffered bytes to the client. Send already
// flushes after each record; call this explicitly to flush an intermediate
// header write or a custom raw write.
func (s *StreamWriter) Flush() error {
	if s.closed {
		return ErrStreamClosed
	}
	s.flush()
	return nil
}

// Close marks the writer as closed. Subsequent Send/Flush calls return
// ErrStreamClosed. Close does not write a final newline; the framers
// already terminate every record.
func (s *StreamWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.started {
		s.flush()
	}
	return nil
}

func (s *StreamWriter) ensureStarted() {
	if s.started {
		return
	}
	s.started = true
	h := s.w.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", s.ct)
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	s.w.WriteHeader(s.status)
}

func (s *StreamWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// SSEWriter writes a text/event-stream response one event at a time.
// Created via *Context.SSE. Each Send renders one SSE frame per the
// WHATWG specification: id:, event:, retry:, then one or more data: lines,
// terminated by a blank line. Multi-line data values are split on \n.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	codec   Codec
	ctx     context.Context
	status  int
	started bool
	closed  bool
	buf     bytes.Buffer
}

func newSSEWriter(c *Context, status int) *SSEWriter {
	flusher, _ := c.Writer.(http.Flusher)
	return &SSEWriter{
		w:       c.Writer,
		flusher: flusher,
		codec:   c.codec,
		ctx:     c.Request.Context(),
		status:  status,
	}
}

// Send writes one SSE event. Data values that are strings or []byte are
// written verbatim (split on '\n' into multiple data: lines per the SSE
// spec); any other value is encoded as JSON via the router codec on a
// single data: line.
func (s *SSEWriter) Send(ev SSEEvent) error {
	if s.closed {
		return ErrStreamClosed
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.ensureStarted()
	s.buf.Reset()

	if ev.Comment != "" {
		writeSSEComment(&s.buf, ev.Comment)
	}
	if ev.ID != "" {
		writeSSEField(&s.buf, "id", ev.ID)
	}
	if ev.Event != "" {
		writeSSEField(&s.buf, "event", ev.Event)
	}
	if ev.Retry > 0 {
		writeSSEField(&s.buf, "retry", strconv.Itoa(ev.Retry))
	}
	if err := writeSSEData(&s.buf, ev.Data, s.codec); err != nil {
		return err
	}
	s.buf.WriteByte('\n')

	if _, err := s.w.Write(s.buf.Bytes()); err != nil {
		return err
	}
	s.flush()
	return nil
}

// Comment is a convenience for sending a keep-alive comment frame.
// Equivalent to Send(SSEEvent{Comment: text}).
func (s *SSEWriter) Comment(text string) error {
	return s.Send(SSEEvent{Comment: text})
}

// Flush flushes the underlying ResponseWriter.
func (s *SSEWriter) Flush() error {
	if s.closed {
		return ErrStreamClosed
	}
	s.flush()
	return nil
}

// Close marks the writer as closed. Subsequent Send calls return
// ErrStreamClosed.
func (s *SSEWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.started {
		s.flush()
	}
	return nil
}

func (s *SSEWriter) ensureStarted() {
	if s.started {
		return
	}
	s.started = true
	h := s.w.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/event-stream")
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	if h.Get("Connection") == "" {
		h.Set("Connection", "keep-alive")
	}
	h.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(s.status)
}

func (s *SSEWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func writeSSEField(buf *bytes.Buffer, name, value string) {
	for _, line := range splitNewlines(value) {
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

func writeSSEComment(buf *bytes.Buffer, comment string) {
	for _, line := range splitNewlines(comment) {
		buf.WriteString(": ")
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
}

func writeSSEData(buf *bytes.Buffer, v any, codec Codec) error {
	switch d := v.(type) {
	case nil:
		writeSSEField(buf, "data", "")
		return nil
	case string:
		writeSSEField(buf, "data", d)
		return nil
	case []byte:
		writeSSEField(buf, "data", string(d))
		return nil
	}
	var enc bytes.Buffer
	if err := codec.Encode(&enc, v); err != nil {
		return err
	}
	payload := strings.TrimRight(enc.String(), "\n")
	writeSSEField(buf, "data", payload)
	return nil
}

func splitNewlines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

// MultipartWriter writes a multipart/mixed (or any multipart/*) response
// one part at a time. Created via *Context.MultipartMixed; the boundary is
// generated at construction time and embedded in the Content-Type header.
type MultipartWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	ctx      context.Context
	status   int
	mediaTyp string
	mw       *multipart.Writer
	started  bool
	closed   bool
}

func newMultipartWriter(
	c *Context,
	status int,
	mediaType string,
) *MultipartWriter {
	flusher, _ := c.Writer.(http.Flusher)
	mp := &MultipartWriter{
		w:        c.Writer,
		flusher:  flusher,
		ctx:      c.Request.Context(),
		status:   status,
		mediaTyp: mediaType,
	}
	mp.mw = multipart.NewWriter(c.Writer)
	mp.mw.SetBoundary(randomBoundary())
	return mp
}

// Boundary returns the boundary string used in the Content-Type header.
func (m *MultipartWriter) Boundary() string {
	return m.mw.Boundary()
}

// Part writes one part with the given headers, copying body into the
// stream. headers should at least include Content-Type for each part.
func (m *MultipartWriter) Part(
	headers textproto.MIMEHeader,
	body io.Reader,
) error {
	if m.closed {
		return ErrStreamClosed
	}
	if err := m.ctx.Err(); err != nil {
		return err
	}
	m.ensureStarted()
	if headers == nil {
		headers = textproto.MIMEHeader{}
	}
	pw, err := m.mw.CreatePart(headers)
	if err != nil {
		return err
	}
	if body != nil {
		if _, err := io.Copy(pw, body); err != nil {
			return err
		}
	}
	m.flush()
	return nil
}

// Flush flushes the underlying ResponseWriter.
func (m *MultipartWriter) Flush() error {
	if m.closed {
		return ErrStreamClosed
	}
	m.flush()
	return nil
}

// Close writes the closing boundary and flushes. The writer cannot be
// reused after Close.
func (m *MultipartWriter) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	if !m.started {
		m.ensureStarted()
	}
	if err := m.mw.Close(); err != nil {
		return err
	}
	m.flush()
	return nil
}

func (m *MultipartWriter) ensureStarted() {
	if m.started {
		return
	}
	m.started = true
	h := m.w.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", fmt.Sprintf(
			"%s; boundary=%s", m.mediaTyp, m.mw.Boundary(),
		))
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	m.w.WriteHeader(m.status)
}

func (m *MultipartWriter) flush() {
	if m.flusher != nil {
		m.flusher.Flush()
	}
}

func randomBoundary() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "minmux-" + hex.EncodeToString(buf[:])
}

// normalizeMediaType strips parameters and lowercases a media type. It is a
// trimmed-down mime.ParseMediaType that doesn't require the full RFC parse
// for the common case of "type/subtype" or "type/subtype; param=value".
func normalizeMediaType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}
