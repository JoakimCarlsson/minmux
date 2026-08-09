package compress

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
)

// encoder is what gzip.Writer and flate.Writer have in common.
type encoder interface {
	io.WriteCloser
	Flush() error
	Reset(io.Writer)
}

// pools keeps one writer pool per encoding and level, so a server that runs
// large bodies through the middleware reuses their window and hash tables
// instead of allocating a fresh compressor per request.
var pools sync.Map

// poolKey identifies one pool: a compressor is only reusable by a response
// that wants the same encoding at the same level.
type poolKey struct {
	encoding string
	level    int
}

// pool returns the pool for one encoding at one level.
func pool(encoding string, level int) *sync.Pool {
	k := poolKey{encoding: encoding, level: level}
	if p, ok := pools.Load(k); ok {
		return p.(*sync.Pool)
	}
	p, _ := pools.LoadOrStore(k, &sync.Pool{New: func() any {
		return newEncoder(encoding, level)
	}})
	return p.(*sync.Pool)
}

// newEncoder builds a compressor. The level has already been validated, so the
// constructors cannot fail here.
func newEncoder(encoding string, level int) encoder {
	if encoding == EncodingDeflate {
		w, _ := flate.NewWriter(io.Discard, level)
		return w
	}
	w, _ := gzip.NewWriterLevel(io.Discard, level)
	return w
}

// responseWriter defers the decision to compress until it has seen both the
// content type and enough of the body to know the encoding will pay for
// itself. Until then the body accumulates in buf and no header has been sent,
// which is what lets the middleware still choose either way.
type responseWriter struct {
	http.ResponseWriter
	opts        Options
	encoding    string
	status      int
	decided     bool
	wroteHeader bool
	passthrough bool
	buf         []byte
	enc         encoder
}

// WriteHeader settles eligibility but not the headers themselves. A handler
// that encoded its own body owns the encoding, a partial response cannot be
// re-encoded without invalidating the ranges it answers, and a status defined
// to carry no body has nothing to compress; everything else is held back until
// the body decides whether these headers will carry Content-Encoding.
//
// A response that could have been compressed gains Vary: Accept-Encoding
// whether or not it ends up compressed, and whether or not this client asked
// for an encoding — the body a shared cache is about to store depends on that
// header either way.
func (w *responseWriter) WriteHeader(status int) {
	if w.decided {
		return
	}
	w.decided = true
	w.status = status
	h := w.Header()
	if h.Get("Content-Encoding") != "" ||
		h.Get("Content-Range") != "" ||
		status == http.StatusPartialContent ||
		status == http.StatusNoContent ||
		status == http.StatusNotModified ||
		!compressible(h.Get("Content-Type"), w.opts.Types) {
		w.commitPlain()
		return
	}
	h.Add("Vary", "Accept-Encoding")
	if w.encoding == "" {
		w.commitPlain()
	}
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(w.status)
	}
	switch {
	case w.passthrough:
		return w.ResponseWriter.Write(p)
	case w.enc != nil:
		return w.enc.Write(p)
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) >= w.opts.MinSize {
		if err := w.commitEncoded(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush settles the response as it stands, so a handler that streams is not
// held in the buffer waiting for a threshold it may never reach. A stream that
// has not yet earned compression is sent uncompressed rather than being
// committed to an encoding on the strength of its first few bytes.
func (w *responseWriter) Flush() {
	if !w.passthrough && w.enc == nil {
		w.commitPlain()
	}
	if w.enc != nil {
		_ = w.enc.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack gives up the connection to the caller, which is what a websocket
// upgrade needs. Anything buffered is sent first: the bytes were accepted from
// the handler and dropping them silently would be worse than sending them
// uncompressed.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New(
			"compress: underlying ResponseWriter is not an http.Hijacker",
		)
	}
	if w.enc != nil {
		_ = w.enc.Close()
		w.release()
	} else if len(w.buf) > 0 {
		w.commitPlain()
	}
	w.passthrough = true
	return hijacker.Hijack()
}

// Unwrap exposes the wrapped writer to http.ResponseController, so the
// capabilities this type does not implement itself still reach the server.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// commitPlain sends what has been held back uncompressed and routes the rest
// of the body straight through.
func (w *responseWriter) commitPlain() {
	w.decided = true
	w.passthrough = true
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(w.status)
	}
	if len(w.buf) > 0 {
		_, _ = w.ResponseWriter.Write(w.buf)
		w.buf = nil
	}
}

// commitEncoded sends what has been held back through the compressor. Any
// length already announced was the uncompressed one and the encoded length is
// not known until the stream closes, so the header goes rather than being
// corrected.
func (w *responseWriter) commitEncoded() error {
	h := w.Header()
	h.Set("Content-Encoding", w.encoding)
	h.Del("Content-Length")
	h.Del("Accept-Ranges")
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(w.status)

	w.enc = pool(w.encoding, w.opts.Level).Get().(encoder)
	w.enc.Reset(w.ResponseWriter)
	if len(w.buf) > 0 {
		buf := w.buf
		w.buf = nil
		if _, err := w.enc.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

// close finishes the stream and returns the compressor to its pool. A handler
// that wrote nothing at all still needs its status sent.
func (w *responseWriter) close() {
	if w.enc != nil {
		_ = w.enc.Close()
		w.release()
		return
	}
	if !w.passthrough {
		w.commitPlain()
	}
}

// release hands the compressor back, detached from this response so the pool
// holds no reference to a finished request's writer.
func (w *responseWriter) release() {
	w.enc.Reset(io.Discard)
	pool(w.encoding, w.opts.Level).Put(w.enc)
	w.enc = nil
}
