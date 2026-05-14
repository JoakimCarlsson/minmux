package router

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
)

// Recover returns middleware that catches panics from downstream handlers,
// logs them with stack via log.Default(), and writes a 500 ProblemDetails
// response. Use as the first Router.Use in a production setup so it
// wraps every other middleware and handler.
//
//	r := router.New()
//	r.Use(router.Recover())
//
// http.ErrAbortHandler is re-panicked so net/http's standard
// connection-close-without-logging behavior is preserved.
//
// If the handler already wrote response headers before panicking, the
// 500 body cannot be cleanly emitted — net/http will have flushed the
// earlier status, and our WriteHeader call becomes a no-op. The panic
// is still logged. For panics in streaming handlers, the connection
// will simply close mid-stream.
func Recover() func(http.Handler) http.Handler {
	return RecoverWith(log.Default())
}

// RecoverWith is Recover with a caller-supplied logger. Passing nil falls
// back to log.Default().
func RecoverWith(logger *log.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = log.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					// Standard signal from handlers that want net/http
					// to close the connection silently. Re-panic so the
					// stdlib's handling kicks in.
					panic(rec)
				}
				logger.Printf(
					"minmux: panic in %s %s: %v\n%s",
					r.Method, r.URL.Path, rec, debug.Stack(),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(InternalServerError(
					"an unexpected error occurred",
				))
			}()
			next.ServeHTTP(w, r)
		})
	}
}
