package main

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// LoginParams binds an application/x-www-form-urlencoded body.
type LoginParams struct {
	Username string `form:"username"`
	Password string `form:"password" format:"password"`
	Remember *bool  `form:"remember"`
}

// ProfileParams binds a multipart/form-data body with text fields and
// a single required file restricted by Content-Type.
type ProfileParams struct {
	DisplayName string           `form:"display_name"`
	Bio         *string          `form:"bio"`
	Avatar      *router.FormFile `                    file:"avatar" contentType:"image/png, image/jpeg"`
}

// GalleryParams binds a multipart/form-data body with a title and
// repeated photo files.
type GalleryParams struct {
	Title  string             `form:"title"`
	Photos []*router.FormFile `             file:"photos"`
}

// RawUploadParams binds the raw request body as a stream. The contentType
// tag both validates the incoming Content-Type and drives the generated
// requestBody.content keys.
type RawUploadParams struct {
	Body io.Reader `body:"" contentType:"image/png, image/jpeg, application/octet-stream"`
}

func login(c *router.Context, p LoginParams) {
	c.JSON(http.StatusOK, map[string]any{
		"username":  p.Username,
		"password":  p.Password,
		"remember":  p.Remember,
		"logged_in": true,
	})
}

func profile(c *router.Context, p ProfileParams) {
	ct := p.Avatar.Header.Get("Content-Type")
	c.JSON(http.StatusCreated, map[string]any{
		"display_name": p.DisplayName,
		"bio":          p.Bio,
		"avatar": map[string]any{
			"filename":     p.Avatar.Filename,
			"size":         p.Avatar.Size,
			"content_type": ct,
		},
	})
}

func gallery(c *router.Context, p GalleryParams) {
	out := make([]map[string]any, len(p.Photos))
	for i, ph := range p.Photos {
		out[i] = map[string]any{
			"filename":     ph.Filename,
			"size":         ph.Size,
			"content_type": ph.Header.Get("Content-Type"),
		}
	}
	c.JSON(http.StatusCreated, map[string]any{
		"title":  p.Title,
		"photos": out,
	})
}

func rawUpload(c *router.Context, p RawUploadParams) {
	n, err := io.Copy(io.Discard, p.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, router.BadRequest(err.Error()))
		return
	}
	c.JSON(http.StatusOK, map[string]any{
		"bytes_received": n,
		"content_type":   c.Request.Header.Get("Content-Type"),
	})
}

// LoginResponse, ProfileResponse, GalleryResponse, RawUploadResponse are
// only used by the OpenAPI generator to describe response payloads.
type LoginResponse struct {
	Username string `json:"username"`
	LoggedIn bool   `json:"logged_in"`
}

type FileInfo struct {
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type ProfileResponse struct {
	DisplayName string   `json:"display_name"`
	Bio         *string  `json:"bio,omitempty"`
	Avatar      FileInfo `json:"avatar"`
}

type GalleryResponse struct {
	Title  string     `json:"title"`
	Photos []FileInfo `json:"photos"`
}

type RawUploadResponse struct {
	BytesReceived int64  `json:"bytes_received"`
	ContentType   string `json:"content_type"`
}

func main() {
	r := router.New(router.WithMaxMultipartMemory(16 << 20))

	r.Post(
		"/login",
		login,
		openapi.Summary("Form-based login"),
		openapi.Description(
			"Authenticates with an application/x-www-form-urlencoded body.",
		),
		openapi.Tags("Forms"),
		openapi.ReturnsBody[LoginResponse](http.StatusOK, "Logged in"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest, "Missing or invalid field",
		),
	)

	r.Post(
		"/profile",
		profile,
		openapi.Summary("Upload a profile with avatar"),
		openapi.Description(
			"Multipart form: display_name + optional bio + a required "+
				"avatar image restricted to PNG or JPEG.",
		),
		openapi.Tags("Forms"),
		openapi.ReturnsBody[ProfileResponse](
			http.StatusCreated, "Profile created",
		),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest, "Missing field or unsupported image type",
		),
	)

	r.Post(
		"/gallery",
		gallery,
		openapi.Summary("Upload a photo gallery"),
		openapi.Description(
			"Multipart form with a title and one or more photos repeated "+
				"under the same `photos` field.",
		),
		openapi.Tags("Forms"),
		openapi.ReturnsBody[GalleryResponse](
			http.StatusCreated, "Gallery created",
		),
	)

	r.Post(
		"/raw",
		rawUpload,
		openapi.Summary("Raw stream upload"),
		openapi.Description(
			"Streams the entire request body without buffering. The "+
				"Content-Type header must match one of the declared types.",
		),
		openapi.Tags("Streams"),
		openapi.ReturnsBody[RawUploadResponse](http.StatusOK, "Bytes received"),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Forms and Uploads",
		Version:     "0.1.0",
		Description: "Smoke test for form / file / stream binding.",
	})
	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	addr := ":8080"
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
