package router

import "net/http"

// ProblemDetails is an RFC 7807 error response. It is the canonical 4xx/5xx
// payload shape, an error in its own right, and carries the HTTP status
// the framework uses when it is returned from a handler.
type ProblemDetails struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// Error implements the error interface.
func (p *ProblemDetails) Error() string {
	if p.Detail != "" {
		return p.Title + ": " + p.Detail
	}
	return p.Title
}

// HTTPStatus implements StatusError.
func (p *ProblemDetails) HTTPStatus() int { return p.Status }

// StatusError is implemented by errors that map to a specific HTTP status.
// Errors returned from handlers that satisfy this interface produce the
// indicated status code; bare errors produce 500.
type StatusError interface {
	error
	HTTPStatus() int
}

// NotFound builds a 404 ProblemDetails.
func NotFound(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusNotFound,
		Title:  http.StatusText(http.StatusNotFound),
		Detail: detail,
	}
}

// BadRequest builds a 400 ProblemDetails.
func BadRequest(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusBadRequest,
		Title:  http.StatusText(http.StatusBadRequest),
		Detail: detail,
	}
}

// Conflict builds a 409 ProblemDetails.
func Conflict(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusConflict,
		Title:  http.StatusText(http.StatusConflict),
		Detail: detail,
	}
}

// Unauthorized builds a 401 ProblemDetails.
func Unauthorized(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusUnauthorized,
		Title:  http.StatusText(http.StatusUnauthorized),
		Detail: detail,
	}
}

// Forbidden builds a 403 ProblemDetails.
func Forbidden(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusForbidden,
		Title:  http.StatusText(http.StatusForbidden),
		Detail: detail,
	}
}
