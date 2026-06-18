package router_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type loginParams struct {
	Username string `form:"username"`
	Password string `form:"password"`
	Remember *bool  `form:"remember"`
}

type uploadParams struct {
	Title  string             `form:"title"`
	Avatar *router.FormFile   `             file:"avatar" contentType:"image/png, image/jpeg"`
	Photos []*router.FormFile `             file:"photos"`
}

type rawReaderParams struct {
	Body io.Reader `body:"" contentType:"image/png, application/octet-stream"`
}

type rawBytesParams struct {
	Body []byte `body:""`
}

func TestBinder_Urlencoded(t *testing.T) {
	r := router.New()
	var got loginParams
	r.Post("/login", func(c *router.Context, p loginParams) {
		got = p
		c.NoContent()
	})

	form := "username=alice&password=secret&remember=true"
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if got.Username != "alice" || got.Password != "secret" {
		t.Errorf("fields: %+v", got)
	}
	if got.Remember == nil || *got.Remember != true {
		t.Errorf("remember: %+v", got.Remember)
	}
}

func TestBinder_UrlencodedMissingRequired(t *testing.T) {
	r := router.New()
	r.Post("/login", func(c *router.Context, p loginParams) {
		c.NoContent()
	})

	form := "username=alice"
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `password`) {
		t.Errorf("expected error to mention password, got %s", rec.Body)
	}
}

type queryReqParams struct {
	ID   string `query:"id,required"`
	Name string `query:"name"`
}

func TestBinder_QueryRequiredIsDocOnly(t *testing.T) {
	r := router.New()
	var got queryReqParams
	r.Get("/items", func(c *router.Context, p queryReqParams) {
		got = p
		c.NoContent()
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/items", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("missing ,required query must not be rejected: %d %s",
			rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/items?id=abc", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if got.ID != "abc" {
		t.Errorf("the ,required modifier must be stripped from the name: %+v", got)
	}
}

func TestBinder_Multipart(t *testing.T) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("title", "vacation")
	writePart(t, mw, "avatar", "me.png", "image/png", []byte("PNGBYTES"))
	writePart(t, mw, "photos", "a.jpg", "image/jpeg", []byte("AAAA"))
	writePart(t, mw, "photos", "b.jpg", "image/jpeg", []byte("BBBB"))
	_ = mw.Close()

	r := router.New()
	var got uploadParams
	r.Post("/upload", func(c *router.Context, p uploadParams) {
		got = p
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if got.Title != "vacation" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Avatar == nil || got.Avatar.Filename != "me.png" {
		t.Fatalf("avatar: %+v", got.Avatar)
	}
	if len(got.Photos) != 2 {
		t.Fatalf("photos: %d", len(got.Photos))
	}
	if got.Photos[0].Filename != "a.jpg" || got.Photos[1].Filename != "b.jpg" {
		t.Errorf("photo names: %q %q",
			got.Photos[0].Filename, got.Photos[1].Filename,
		)
	}
	f, err := got.Avatar.Open()
	if err != nil {
		t.Fatalf("avatar open: %v", err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if string(data) != "PNGBYTES" {
		t.Errorf("avatar content: %q", data)
	}
}

func TestBinder_MultipartDisallowedContentType(t *testing.T) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("title", "vacation")
	writePart(t, mw, "avatar", "me.gif", "image/gif", []byte("GIFBYTES"))
	_ = mw.Close()

	r := router.New()
	r.Post("/upload", func(c *router.Context, p uploadParams) {
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "not allowed") {
		t.Errorf("expected content type rejection, got %s", rec.Body)
	}
}

func TestBinder_MultipartMissingRequiredFile(t *testing.T) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("title", "vacation")
	_ = mw.Close()

	r := router.New()
	r.Post("/upload", func(c *router.Context, p uploadParams) {
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `avatar`) {
		t.Errorf("expected avatar required error, got %s", rec.Body)
	}
}

func TestBinder_OctetStreamReader(t *testing.T) {
	r := router.New()
	var got []byte
	r.Post("/raw", func(c *router.Context, p rawReaderParams) {
		data, err := io.ReadAll(p.Body)
		if err != nil {
			c.JSON(
				http.StatusInternalServerError,
				router.BadRequest(err.Error()),
			)
			return
		}
		got = data
		c.NoContent()
	})

	req := httptest.NewRequest(
		"POST",
		"/raw",
		bytes.NewReader([]byte("PNGDATA")),
	)
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if string(got) != "PNGDATA" {
		t.Errorf("body: %q", got)
	}
}

func TestBinder_OctetStreamWrongContentType(t *testing.T) {
	r := router.New()
	r.Post("/raw", func(c *router.Context, p rawReaderParams) {
		c.NoContent()
	})

	req := httptest.NewRequest("POST", "/raw", bytes.NewReader([]byte("X")))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
}

func TestBinder_BytesBody(t *testing.T) {
	r := router.New()
	var got []byte
	r.Post("/bytes", func(c *router.Context, p rawBytesParams) {
		got = p.Body
		c.NoContent()
	})

	req := httptest.NewRequest(
		"POST",
		"/bytes",
		bytes.NewReader([]byte("HELLO")),
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if string(got) != "HELLO" {
		t.Errorf("body: %q", got)
	}
}

func TestBinder_BytesBodyExceedsLimit(t *testing.T) {
	r := router.New(router.WithMaxMultipartMemory(4))
	r.Post("/bytes", func(c *router.Context, p rawBytesParams) {
		c.NoContent()
	})

	req := httptest.NewRequest(
		"POST",
		"/bytes",
		bytes.NewReader([]byte("TOOBIG")),
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
}

type mixedBodyParams struct {
	Body []byte `body:""`
	Name string `        form:"name"`
}

func TestBinder_ExclusiveTagsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when body and form share a struct")
		}
	}()
	r := router.New()
	r.Post("/x", func(c *router.Context, p mixedBodyParams) {})
}

type loginCommand struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type jsonBodyParams struct {
	Body loginCommand `body:""`
}

func TestBinder_JSONBodyStillWorks(t *testing.T) {
	r := router.New()
	var got loginCommand
	r.Post("/json", func(c *router.Context, p jsonBodyParams) {
		got = p.Body
		c.NoContent()
	})

	raw, _ := json.Marshal(loginCommand{Username: "u", Password: "p"})
	req := httptest.NewRequest("POST", "/json", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body)
	}
	if got.Username != "u" || got.Password != "p" {
		t.Errorf("body: %+v", got)
	}
}

func writePart(
	t *testing.T,
	mw *multipart.Writer,
	field, filename, ct string,
	content []byte,
) {
	t.Helper()
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		`form-data; name="`+field+`"; filename="`+filename+`"`,
	)
	h.Set("Content-Type", ct)
	w, err := mw.CreatePart(h)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
}
