package router

import "net/http"

// Response is implemented by any typed result wrapper. When a handler returns
// a value satisfying Response, the framework calls its WriteResponse method
// instead of serializing the value as a generic 200 body.
type Response interface {
	WriteResponse(w http.ResponseWriter, c Codec) error
}

// Ok wraps a value as a 200 OK response with a JSON body.
type Ok[T any] struct {
	Value T
}

// WriteResponse implements Response.
func (r Ok[T]) WriteResponse(w http.ResponseWriter, c Codec) error {
	w.Header().Set("Content-Type", c.ContentType())
	w.WriteHeader(http.StatusOK)
	return c.Encode(w, r.Value)
}

// Created wraps a value as a 201 Created response with an optional Location
// header.
type Created[T any] struct {
	Value    T
	Location string
}

// WriteResponse implements Response.
func (r Created[T]) WriteResponse(w http.ResponseWriter, c Codec) error {
	if r.Location != "" {
		w.Header().Set("Location", r.Location)
	}
	w.Header().Set("Content-Type", c.ContentType())
	w.WriteHeader(http.StatusCreated)
	return c.Encode(w, r.Value)
}

// NoContent is a 204 No Content response.
type NoContent struct{}

// WriteResponse implements Response.
func (NoContent) WriteResponse(w http.ResponseWriter, _ Codec) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Redirect is a 3xx redirect response. Status defaults to 303 See Other.
type Redirect struct {
	URL    string
	Status int
}

// WriteResponse implements Response.
func (r Redirect) WriteResponse(w http.ResponseWriter, _ Codec) error {
	status := r.Status
	if status == 0 {
		status = http.StatusSeeOther
	}
	w.Header().Set("Location", r.URL)
	w.WriteHeader(status)
	return nil
}
