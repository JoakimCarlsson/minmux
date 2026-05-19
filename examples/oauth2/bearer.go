package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(router.ProblemDetails{
		Type: "about:blank", Title: http.StatusText(status),
		Status: status, Detail: detail,
	})
}

// TokenResponse is the success body shared by every /token endpoint.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

// TokenError is RFC 6749 §5.2's error body.
type TokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// DeviceCodeResponse is RFC 8628 §3.2's device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// accessToken is the in-memory record for every bearer token issued
// regardless of which flow produced it.
type accessToken struct {
	subject   string
	clientID  string
	scopes    []string
	expiresAt time.Time
}

// principal is what handlers see in their request context after the
// bearer middleware has validated the token.
type principal struct {
	subject  string
	clientID string
	scopes   []string
}

type ctxKey struct{}

// requireToken validates the Bearer header against the in-memory token
// table and attaches a *principal to the request context. Per-route
// scope enforcement is layered on top via requireScope inside handlers.
func (s *store) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeProblem(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		raw := strings.TrimPrefix(h, "Bearer ")

		s.mu.Lock()
		tok, ok := s.tokens[raw]
		s.mu.Unlock()
		if !ok || time.Now().After(tok.expiresAt) {
			writeProblem(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		p := &principal{
			subject:  tok.subject,
			clientID: tok.clientID,
			scopes:   tok.scopes,
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireScope returns false (and writes a 403) when the principal is
// missing the requested scope. Handlers call this at the top.
func requireScope(c *router.Context, scope string) bool {
	p, _ := c.Request.Context().Value(ctxKey{}).(*principal)
	if p == nil || !slices.Contains(p.scopes, scope) {
		c.JSON(http.StatusForbidden, router.ProblemDetails{
			Type: "about:blank", Title: "Forbidden",
			Status: http.StatusForbidden, Detail: "missing scope " + scope,
		})
		return false
	}
	return true
}

func principalFrom(c *router.Context) *principal {
	p, _ := c.Request.Context().Value(ctxKey{}).(*principal)
	return p
}

// randomToken returns a URL-safe base64 string of n random bytes.
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenErr(c *router.Context, code string) {
	c.JSON(http.StatusBadRequest, TokenError{Error: code})
}

// issueToken records a new access token and returns the opaque value
// the client should present in the Authorization header.
func (s *store) issueToken(subject, clientID string, scopes []string) string {
	tok := randomToken(32)
	s.mu.Lock()
	s.tokens[tok] = accessToken{
		subject:   subject,
		clientID:  clientID,
		scopes:    scopes,
		expiresAt: time.Now().Add(accessTTL),
	}
	s.mu.Unlock()
	return tok
}
