package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

const authCodeClientID = "spa-app"

type authCode struct {
	clientID            string
	redirectURI         string
	scopes              []string
	codeChallenge       string
	codeChallengeMethod string
	expiresAt           time.Time
}

type Profile struct {
	Subject string   `json:"subject"`
	Scopes  []string `json:"scopes"`
}

type TodoItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type NewTodo struct {
	Title string `json:"title" minLength:"1" maxLength:"200"`
}

type CreateTodoParams struct {
	Body NewTodo `body:""`
}

func profile(c *router.Context) {
	if !requireScope(c, "profile:read") {
		return
	}
	p := principalFrom(c)
	c.JSON(http.StatusOK, Profile{Subject: p.subject, Scopes: p.scopes})
}

func createTodo(c *router.Context, p CreateTodoParams) {
	if !requireScope(c, "todos:write") {
		return
	}
	c.JSON(http.StatusCreated, TodoItem{
		ID:    fmt.Sprintf("todo-%d", time.Now().UnixNano()),
		Title: p.Body.Title,
	})
}

var consentTpl = template.Must(template.New("consent").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Authorize</title>
<style>body{font-family:system-ui;max-width:480px;margin:40px auto;padding:24px;border:1px solid #ccc;border-radius:8px}
.scope{padding:4px 8px;background:#eef;border-radius:4px;margin:2px;display:inline-block}
button{padding:8px 16px;margin-right:8px}</style></head>
<body>
<h2>Authorize <code>{{.ClientID}}</code></h2>
<p>Wants access with these scopes:</p>
<p>{{range .Scopes}}<span class="scope">{{.}}</span>{{end}}</p>
<form method="POST" action="/oauth/auth-code/authorize">
{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
<button name="decision" value="allow">Allow</button>
<button name="decision" value="deny" formnovalidate>Deny</button>
</form>
</body></html>`))

func (s *store) authCodeAuthorizeGET(c *router.Context) {
	q := c.Request.URL.Query()
	if q.Get("response_type") != "code" {
		http.Error(c.Writer, "unsupported_response_type", http.StatusBadRequest)
		return
	}
	if q.Get("client_id") != authCodeClientID {
		http.Error(c.Writer, "invalid_client", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		http.Error(c.Writer, "PKCE S256 required", http.StatusBadRequest)
		return
	}
	scopes := strings.Fields(q.Get("scope"))
	hidden := map[string]string{
		"client_id":             q.Get("client_id"),
		"redirect_uri":          q.Get("redirect_uri"),
		"state":                 q.Get("state"),
		"scope":                 q.Get("scope"),
		"code_challenge":        q.Get("code_challenge"),
		"code_challenge_method": q.Get("code_challenge_method"),
	}
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = consentTpl.Execute(c.Writer, map[string]any{
		"ClientID": q.Get("client_id"),
		"Scopes":   scopes,
		"Hidden":   hidden,
	})
}

func (s *store) authCodeAuthorizePOST(c *router.Context) {
	_ = c.Request.ParseForm()
	redirectURI := c.Request.FormValue("redirect_uri")
	state := c.Request.FormValue("state")

	if c.Request.FormValue("decision") != "allow" {
		redirectErr(c, redirectURI, state, "access_denied")
		return
	}

	code := randomToken(24)
	s.mu.Lock()
	s.codes[code] = authCode{
		clientID:            c.Request.FormValue("client_id"),
		redirectURI:         redirectURI,
		scopes:              strings.Fields(c.Request.FormValue("scope")),
		codeChallenge:       c.Request.FormValue("code_challenge"),
		codeChallengeMethod: c.Request.FormValue("code_challenge_method"),
		expiresAt:           time.Now().Add(codeTTL),
	}
	s.mu.Unlock()

	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(c.Writer, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

func (s *store) authCodeToken(c *router.Context) {
	_ = c.Request.ParseForm()
	if c.Request.FormValue("grant_type") != "authorization_code" {
		tokenErr(c, "unsupported_grant_type")
		return
	}
	code := c.Request.FormValue("code")

	s.mu.Lock()
	rec, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()

	if !ok || time.Now().After(rec.expiresAt) {
		tokenErr(c, "invalid_grant")
		return
	}
	if c.Request.FormValue("client_id") != rec.clientID || c.Request.FormValue("redirect_uri") != rec.redirectURI {
		tokenErr(c, "invalid_grant")
		return
	}
	if !verifyPKCE(c.Request.FormValue("code_verifier"), rec.codeChallenge) {
		tokenErr(c, "invalid_grant")
		return
	}

	tok := s.issueToken("user-42", rec.clientID, rec.scopes)
	c.JSON(http.StatusOK, TokenResponse{
		AccessToken: tok,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessTTL.Seconds()),
		Scope:       strings.Join(rec.scopes, " "),
	})
}

func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func redirectErr(c *router.Context, redirectURI, state, code string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(c.Writer, code, http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}
