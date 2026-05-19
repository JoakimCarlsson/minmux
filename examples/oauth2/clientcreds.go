package main

import (
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"

	"github.com/joakimcarlsson/minmux/router"
)

// clientRegistry is the static registry of confidential clients allowed
// to use the client_credentials grant. Real systems load this from a DB
// and store hashed secrets; we use constant-time comparison anyway so
// the comparison cost is consistent regardless of input length.
var clientRegistry = map[string]struct {
	secret string
	scopes []string
}{
	"svc-reporter": {
		secret: "s3cret",
		scopes: []string{"metrics:read", "metrics:write"},
	},
}

type Metric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func listMetrics(c *router.Context) {
	if !requireScope(c, "metrics:read") {
		return
	}
	c.JSON(http.StatusOK, []Metric{
		{Name: "requests_total", Value: 1234},
		{Name: "errors_total", Value: 12},
	})
}

func writeMetric(c *router.Context) {
	if !requireScope(c, "metrics:write") {
		return
	}
	c.JSON(http.StatusCreated, Metric{Name: "accepted", Value: 1})
}

func (s *store) clientCredsToken(c *router.Context) {
	_ = c.Request.ParseForm()
	if c.Request.FormValue("grant_type") != "client_credentials" {
		tokenErr(c, "unsupported_grant_type")
		return
	}

	// RFC 6749 §2.3.1: credentials MAY arrive as HTTP Basic or in the
	// request body. Accept both, with Basic taking precedence.
	id, secret, ok := c.Request.BasicAuth()
	if !ok {
		id = c.Request.FormValue("client_id")
		secret = c.Request.FormValue("client_secret")
	}
	if id == "" {
		tokenErr(c, "invalid_client")
		return
	}

	cl, found := clientRegistry[id]
	if !found ||
		subtle.ConstantTimeCompare([]byte(secret), []byte(cl.secret)) != 1 {
		tokenErr(c, "invalid_client")
		return
	}

	requested := strings.Fields(c.Request.FormValue("scope"))
	granted := requested
	if len(requested) == 0 {
		granted = cl.scopes
	} else {
		for _, sc := range requested {
			if !slices.Contains(cl.scopes, sc) {
				tokenErr(c, "invalid_scope")
				return
			}
		}
	}

	tok := s.issueToken("", id, granted)
	c.JSON(http.StatusOK, TokenResponse{
		AccessToken: tok,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessTTL.Seconds()),
		Scope:       strings.Join(granted, " "),
	})
}
