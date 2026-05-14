package router

import (
	"context"
	"io"
	"net/http"
)

// Context wraps a request and response writer with convenience helpers
// for writing the response. Handlers receive a *Context as their first
// argument and write the response via its methods.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	codec   Codec
}

// Ctx returns the request's context.Context, cancelled when the client
// disconnects or the server shuts down.
func (c *Context) Ctx() context.Context {
	return c.Request.Context()
}

// JSON writes the status code and JSON-encoded body.
func (c *Context) JSON(status int, body any) {
	c.Writer.Header().Set("Content-Type", c.codec.ContentType())
	c.Writer.WriteHeader(status)
	_ = c.codec.Encode(c.Writer, body)
}

// String writes the status code and a plain-text body.
func (c *Context) String(status int, body string) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Writer.WriteHeader(status)
	_, _ = io.WriteString(c.Writer, body)
}

// Bytes writes the status code, content type, and raw body bytes.
func (c *Context) Bytes(status int, contentType string, body []byte) {
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.WriteHeader(status)
	_, _ = c.Writer.Write(body)
}

// Status writes only the status code, no body.
func (c *Context) Status(status int) {
	c.Writer.WriteHeader(status)
}

// NoContent writes 204 with no body.
func (c *Context) NoContent() {
	c.Writer.WriteHeader(http.StatusNoContent)
}

// Header sets a response header.
func (c *Context) Header(key, value string) {
	c.Writer.Header().Set(key, value)
}

// Redirect writes a redirect response with the given Location header.
func (c *Context) Redirect(status int, location string) {
	c.Writer.Header().Set("Location", location)
	c.Writer.WriteHeader(status)
}

// Stream returns a writer for a sequential media-type response such as
// application/jsonl, application/x-ndjson, or application/json-seq. Each
// Send call encodes one item using the router's Codec, writes the framing
// bytes, and flushes if the underlying ResponseWriter supports
// http.Flusher.
//
// Always call Close (typically via defer) once writing is done.
//
//	w := c.Stream(http.StatusOK, "application/jsonl")
//	defer w.Close()
//	for ev := range source {
//	    if err := w.Send(ev); err != nil { return }
//	}
func (c *Context) Stream(status int, contentType string) *StreamWriter {
	return newStreamWriter(c, status, contentType)
}

// SSE returns a writer for a text/event-stream response. Headers
// (Content-Type, Cache-Control: no-cache, Connection: keep-alive,
// X-Accel-Buffering: no) are written on the first Send.
//
//	sse := c.SSE(http.StatusOK)
//	defer sse.Close()
//	sse.Send(router.SSEEvent{Event: "token", Data: tok})
func (c *Context) SSE(status int) *SSEWriter {
	return newSSEWriter(c, status)
}

// MultipartMixed returns a writer for a multipart/mixed response. The
// boundary is generated and set on the Content-Type header at the first
// Part call.
//
//	mp := c.MultipartMixed(http.StatusOK)
//	defer mp.Close()
//	mp.Part(textproto.MIMEHeader{"Content-Type": {"image/png"}}, pngReader)
func (c *Context) MultipartMixed(status int) *MultipartWriter {
	return newMultipartWriter(c, status, "multipart/mixed")
}

// Multipart is the lower-level form of MultipartMixed, taking an explicit
// media type (e.g. "multipart/byteranges"). The boundary is appended
// automatically.
func (c *Context) Multipart(status int, mediaType string) *MultipartWriter {
	return newMultipartWriter(c, status, mediaType)
}
