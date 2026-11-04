package router

import (
	"errors"
	"net/http"
)

// ErrRequired is the cause of a BindError for a required form or file field
// that the request did not carry.
var ErrRequired = errors.New("required")

// BindError reports a request input that could not be bound to a handler's
// Params struct. It is the error a WithBindError hook receives for every
// binding failure, so the hook can tell the failures apart by Source and by
// the cause it wraps, for example a *json.SyntaxError from a body that is
// not valid JSON or a *strconv.NumError from a parameter that does not
// parse.
type BindError struct {
	// Source is where the input came from: "path", "query", "header",
	// "form", "file" or "body".
	Source string
	// Name is the parameter, header or field name, or empty for the body
	// and for a form that could not be parsed at all.
	Name string
	// Err is the underlying cause.
	Err error

	problem *ProblemDetails
}

// newBindError builds the BindError for a failed input from src named name.
// label prefixes the cause in the default problem's detail, giving
// "<label>: <cause>".
func newBindError(
	src fieldSource,
	name, label string,
	err error,
) *BindError {
	return &BindError{
		Source:  src.String(),
		Name:    name,
		Err:     err,
		problem: BadRequest(label + ": " + err.Error()),
	}
}

// Error returns the default problem's title and detail.
func (e *BindError) Error() string {
	return e.problem.Error()
}

// Unwrap returns the underlying cause, so errors.Is and errors.As see it.
func (e *BindError) Unwrap() error {
	return e.Err
}

// Problem returns the 400 ProblemDetails the router writes for e when no
// WithBindError hook is set. Each call returns a fresh copy, so a hook may
// change it and return it.
func (e *BindError) Problem() *ProblemDetails {
	p := *e.problem
	return &p
}

// defaultBindProblem returns the ProblemDetails written for err when no
// WithBindError hook decides otherwise: the BindError's own problem, or a
// generic 400 carrying err's message for any other error.
func defaultBindProblem(err error) *ProblemDetails {
	var be *BindError
	if errors.As(err, &be) {
		return be.Problem()
	}
	return &ProblemDetails{
		Status: http.StatusBadRequest,
		Title:  http.StatusText(http.StatusBadRequest),
		Detail: err.Error(),
	}
}
