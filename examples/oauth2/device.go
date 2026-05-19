package main

import (
	"crypto/rand"
	"html/template"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

const (
	deviceClientID = "tv-app"
	userCodeLen    = 8
)

type deviceState int

const (
	devicePending deviceState = iota
	deviceApproved
	deviceDenied
)

type deviceGrant struct {
	deviceCode string
	userCode   string
	clientID   string
	scopes     []string
	state      deviceState
	subject    string
	expiresAt  time.Time
	nextPoll   time.Time
}

type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func listChannels(c *router.Context) {
	if !requireScope(c, "channels:read") {
		return
	}
	c.JSON(http.StatusOK, []Channel{
		{ID: "ch-1", Name: "News"},
		{ID: "ch-2", Name: "Sports"},
	})
}

func (s *store) deviceAuth(c *router.Context) {
	_ = c.Request.ParseForm()
	if c.Request.FormValue("client_id") != deviceClientID {
		tokenErr(c, "invalid_client")
		return
	}

	scopes := strings.Fields(c.Request.FormValue("scope"))
	if len(scopes) == 0 {
		scopes = []string{"channels:read"}
	}

	g := &deviceGrant{
		deviceCode: randomToken(32),
		userCode:   userCode(),
		clientID:   deviceClientID,
		scopes:     scopes,
		state:      devicePending,
		expiresAt:  time.Now().Add(deviceTTL),
		// First poll is allowed immediately; subsequent polls must respect interval.
		nextPoll: time.Now(),
	}

	s.mu.Lock()
	s.byDeviceCode[g.deviceCode] = g
	s.byUserCode[g.userCode] = g
	s.mu.Unlock()

	c.JSON(http.StatusOK, DeviceCodeResponse{
		DeviceCode:              g.deviceCode,
		UserCode:                g.userCode,
		VerificationURI:         issuer + "/device",
		VerificationURIComplete: issuer + "/device?user_code=" + g.userCode,
		ExpiresIn:               int(deviceTTL.Seconds()),
		Interval:                int(pollMin.Seconds()),
	})
}

// verifyTpl renders the /device page. The empty state explains where the
// user_code comes from and offers a one-click button that starts a demo
// flow locally so visitors can exercise the whole grant from a browser.
var verifyTpl = template.Must(template.New("verify").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Activate device</title>
<style>body{font-family:system-ui;max-width:520px;margin:40px auto;padding:24px;border:1px solid #ccc;border-radius:8px}
input{padding:8px;font-size:16px;letter-spacing:2px;width:100%;box-sizing:border-box;margin:8px 0}
.scope{padding:4px 8px;background:#eef;border-radius:4px;margin:2px;display:inline-block}
button{padding:8px 16px;margin-right:8px}
.msg{padding:8px;border-radius:4px;margin:8px 0}
.ok{background:#dfd}.err{background:#fdd}
.hint{background:#f5f5f5;border-radius:4px;padding:12px;font-size:14px;margin:16px 0}
.hint code{background:#fff;padding:1px 4px;border-radius:3px;font-size:13px}
pre{background:#fff;border:1px solid #ddd;padding:8px;border-radius:4px;font-size:12px;overflow:auto}
#demo-out{margin-top:12px}</style></head>
<body>
<h2>Activate device</h2>
{{if .Message}}<div class="msg {{.MsgClass}}">{{.Message}}</div>{{end}}
{{if .Grant}}
<p>App <code>{{.Grant.ClientID}}</code> wants:</p>
<p>{{range .Grant.Scopes}}<span class="scope">{{.}}</span>{{end}}</p>
<form method="POST" action="/device">
<input type="hidden" name="user_code" value="{{.Grant.UserCode}}">
<button name="decision" value="allow">Allow</button>
<button name="decision" value="deny" formnovalidate>Deny</button>
</form>
{{else}}
<form method="GET" action="/device">
<label>Enter the code shown on your device:</label>
<input name="user_code" value="{{.UserCode}}" autofocus autocomplete="off">
<button>Continue</button>
</form>
<div class="hint">
<p><strong>Where does the code come from?</strong> The device (TV / CLI) first calls <code>POST /oauth/device/device_authorization</code>, receives a <code>user_code</code>, and shows it on its screen. You then type that code here.</p>
<p>To simulate a device from this browser, click the button — it calls the endpoint, fills the code in for you, and prints the poll command:</p>
<button id="demo-btn" type="button">Start a demo device flow</button>
<div id="demo-out"></div>
</div>
<script>
document.getElementById('demo-btn').addEventListener('click', async () => {
  const out = document.getElementById('demo-out');
  out.textContent = 'starting…';
  try {
    const r = await fetch('/oauth/device/device_authorization', {
      method: 'POST',
      headers: {'Content-Type': 'application/x-www-form-urlencoded'},
      body: 'client_id=tv-app&scope=channels:read'
    });
    const j = await r.json();
    document.querySelector('input[name=user_code]').value = j.user_code;
    out.innerHTML =
      '<p>user_code: <code>' + j.user_code + '</code> &middot; ' +
      'device_code: <code>' + j.device_code.slice(0, 12) + '…</code></p>' +
      '<p>Poll for the token from your device:</p>' +
      '<pre>curl -X POST ' + location.origin + '/oauth/device/token \\\n' +
      '  -d grant_type=urn:ietf:params:oauth:grant-type:device_code \\\n' +
      '  -d client_id=tv-app \\\n' +
      '  -d device_code=' + j.device_code + '</pre>' +
      '<p>Click <b>Continue</b> above to approve the request.</p>';
  } catch (e) {
    out.textContent = 'error: ' + e;
  }
});
</script>
{{end}}
</body></html>`))

type verifyView struct {
	UserCode string
	Message  string
	MsgClass string
	Grant    *verifyGrant
}

type verifyGrant struct {
	ClientID string
	Scopes   []string
	UserCode string
}

func (s *store) deviceVerifyGET(c *router.Context) {
	uc := strings.ToUpper(
		strings.TrimSpace(c.Request.URL.Query().Get("user_code")),
	)
	view := verifyView{UserCode: uc}
	if uc != "" {
		s.mu.Lock()
		g := s.byUserCode[uc]
		s.mu.Unlock()
		switch {
		case g == nil || time.Now().After(g.expiresAt):
			view.Message = "Unknown or expired code."
			view.MsgClass = "err"
		case g.state != devicePending:
			view.Message = "Code already used."
			view.MsgClass = "err"
		default:
			view.Grant = &verifyGrant{
				ClientID: g.clientID,
				Scopes:   g.scopes,
				UserCode: g.userCode,
			}
		}
	}
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = verifyTpl.Execute(c.Writer, view)
}

func (s *store) deviceVerifyPOST(c *router.Context) {
	_ = c.Request.ParseForm()
	uc := strings.ToUpper(strings.TrimSpace(c.Request.FormValue("user_code")))
	decision := c.Request.FormValue("decision")

	s.mu.Lock()
	g := s.byUserCode[uc]
	if g != nil && g.state == devicePending && time.Now().Before(g.expiresAt) {
		if decision == "allow" {
			g.state = deviceApproved
			g.subject = "user-99"
		} else {
			g.state = deviceDenied
		}
	}
	s.mu.Unlock()

	view := verifyView{}
	switch {
	case g == nil:
		view.Message = "Unknown code."
		view.MsgClass = "err"
	case g.state == deviceApproved:
		view.Message = "Device activated. You can close this tab."
		view.MsgClass = "ok"
	default:
		view.Message = "Authorization denied."
		view.MsgClass = "err"
	}
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = verifyTpl.Execute(c.Writer, view)
}

func (s *store) deviceToken(c *router.Context) {
	_ = c.Request.ParseForm()
	if c.Request.FormValue(
		"grant_type",
	) != "urn:ietf:params:oauth:grant-type:device_code" {
		tokenErr(c, "unsupported_grant_type")
		return
	}
	dc := c.Request.FormValue("device_code")
	if c.Request.FormValue("client_id") != deviceClientID || dc == "" {
		tokenErr(c, "invalid_client")
		return
	}

	s.mu.Lock()
	g := s.byDeviceCode[dc]
	now := time.Now()
	var (
		errCode string
		grantOK *deviceGrant
	)
	switch {
	case g == nil:
		errCode = "expired_token"
	case now.After(g.expiresAt):
		delete(s.byDeviceCode, g.deviceCode)
		delete(s.byUserCode, g.userCode)
		errCode = "expired_token"
	case now.Before(g.nextPoll):
		errCode = "slow_down"
		g.nextPoll = now.Add(pollMin + time.Second)
	case g.state == devicePending:
		errCode = "authorization_pending"
		g.nextPoll = now.Add(pollMin)
	case g.state == deviceDenied:
		errCode = "access_denied"
		delete(s.byDeviceCode, g.deviceCode)
		delete(s.byUserCode, g.userCode)
	case g.state == deviceApproved:
		grantOK = g
		delete(s.byDeviceCode, g.deviceCode)
		delete(s.byUserCode, g.userCode)
	}
	s.mu.Unlock()

	if grantOK == nil {
		tokenErr(c, errCode)
		return
	}

	tok := s.issueToken(grantOK.subject, grantOK.clientID, grantOK.scopes)
	c.JSON(http.StatusOK, TokenResponse{
		AccessToken: tok,
		TokenType:   "Bearer",
		ExpiresIn:   int(accessTTL.Seconds()),
		Scope:       strings.Join(grantOK.scopes, " "),
	})
}

// userCode generates an RFC 8628 §6.1 friendly code: uppercase
// alphanumerics minus visually ambiguous glyphs, dash-grouped.
func userCode() string {
	const alphabet = "BCDFGHJKLMNPQRSTVWXZ"
	var b strings.Builder
	for i := range userCodeLen {
		if i == userCodeLen/2 {
			b.WriteByte('-')
		}
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String()
}
