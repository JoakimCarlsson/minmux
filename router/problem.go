package router

import "net/http"

// ProblemDetails is an RFC 7807 error response shape. It implements the
// error interface so it can be returned from service-layer code and
// written directly via c.JSON(pd.Status, pd).
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

// InternalServerError builds a 500 ProblemDetails. Used by Recover when
// catching panics from downstream handlers.
func InternalServerError(detail string) *ProblemDetails {
	return &ProblemDetails{
		Status: http.StatusInternalServerError,
		Title:  http.StatusText(http.StatusInternalServerError),
		Detail: detail,
	}
}
