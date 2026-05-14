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
