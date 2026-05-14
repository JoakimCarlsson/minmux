// scalar-ui is a showcase wired up specifically to exercise the breadth
// of the openapi package — full parameter binding, string formats,
// ProblemDetails error responses, every OAS 3.2 security scheme (incl.
// the new deviceAuthorization flow), and SSE / JSONL / multipart/mixed
// streams. Open /docs to browse the result in Scalar.
package main

import (
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
	"github.com/joakimcarlsson/minmux/scalar"
)

type Pet struct {
	ID        string    `json:"id"            format:"uuid"`
	Name      string    `json:"name"                        minLength:"1" maxLength:"100"`
	Tag       string    `json:"tag,omitempty"                                             enum:"dog,cat,bird,fish"`
	CreatedAt time.Time `json:"created_at"`
}

type CreatePetCommand struct {
	Name string `json:"name"          minLength:"1" maxLength:"100" pattern:"^[A-Za-z ]+$"`
	Tag  string `json:"tag,omitempty"                                                      enum:"dog,cat,bird,fish"`
}

type ListPetsParams struct {
	// Tag is optional (pointer) and constrained to a known set of values.
	Tag *string `query:"tag"    enum:"dog,cat,bird,fish"`
	// Limit is required, bounded, with a default.
	Limit int `query:"limit"                           minimum:"1" maximum:"100" default:"20"`
	// Cursor is deprecated; renderers strike it through.
	Cursor *string `query:"cursor"                                                                 deprecated:"true"`
	// TraceID is a required header with a UUID format constraint.
	TraceID string `                                                                                                 header:"X-Trace-Id" format:"uuid"`
}

type GetPetParams struct {
	ID string `path:"id" format:"uuid"`
}

type CreatePetParams struct {
	Body CreatePetCommand `body:""`
}

type DeletePetParams struct {
	ID string `path:"id" format:"uuid"`
}

type User struct {
	ID    string `json:"id"    format:"uuid"`
	Email string `json:"email" format:"email"`
	Name  string `json:"name"`
}

type ChangePasswordCommand struct {
	Current string `json:"current" format:"password"`
	New     string `json:"new"     format:"password"`
}

type ChangePasswordParams struct {
	Body ChangePasswordCommand `body:""`
}

type Token struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type FrameMeta struct {
	Index    int       `json:"index"`
	Captured time.Time `json:"captured"`
}

type IngestParams struct {
	Logs iter.Seq2[LogEntry, error] `body:"" contentType:"application/jsonl, application/x-ndjson"`
}

type IngestReport struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

func health(c *router.Context) {
	c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func me(c *router.Context) {
	c.JSON(http.StatusOK, User{
		ID:    "00000000-0000-0000-0000-000000000000",
		Email: "joe@example.com",
		Name:  "Joe",
	})
}

func changePassword(c *router.Context, _ ChangePasswordParams) {
	c.NoContent()
}

func listPets(c *router.Context, _ ListPetsParams) {
	c.JSON(http.StatusOK, []Pet{
		{
			ID:        "11111111-1111-1111-1111-111111111111",
			Name:      "Fido",
			Tag:       "dog",
			CreatedAt: time.Now().UTC(),
		},
		{
			ID:        "22222222-2222-2222-2222-222222222222",
			Name:      "Whiskers",
			Tag:       "cat",
			CreatedAt: time.Now().UTC(),
		},
	})
}

func getPet(c *router.Context, p GetPetParams) {
	c.JSON(
		http.StatusOK,
		Pet{ID: p.ID, Name: "Fido", Tag: "dog", CreatedAt: time.Now().UTC()},
	)
}

func createPet(c *router.Context, p CreatePetParams) {
	c.JSON(http.StatusCreated, Pet{
		ID:        "33333333-3333-3333-3333-333333333333",
		Name:      p.Body.Name,
		Tag:       p.Body.Tag,
		CreatedAt: time.Now().UTC(),
	})
}

func deletePet(c *router.Context, _ DeletePetParams) {
	c.NoContent()
}

func deviceLogin(c *router.Context) {
	c.JSON(http.StatusOK, map[string]string{
		"device_code":      "GmRhmhcxhwAzkoEqiMEg_DnyEysNkuNhszIySk9eS",
		"user_code":        "WDJB-MJHT",
		"verification_uri": "https://auth.example/device",
	})
}

func tokens(c *router.Context) {
	sse := c.SSE(http.StatusOK)
	defer sse.Close()
	for i, word := range strings.Fields("hello from minmux streaming") {
		if err := sse.Send(router.SSEEvent{
			ID:    fmt.Sprintf("%d", i),
			Event: "token",
			Data:  Token{Index: i, Text: word},
		}); err != nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = sse.Send(router.SSEEvent{Event: "done", Data: ""})
}

func logsJSONL(c *router.Context) {
	w := c.Stream(http.StatusOK, "application/jsonl")
	defer w.Close()
	for i := 0; i < 5; i++ {
		if err := w.Send(LogEntry{
			Timestamp: time.Now().UTC(),
			Level:     "info",
			Message:   fmt.Sprintf("tick %d", i),
		}); err != nil {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func ingest(c *router.Context, p IngestParams) {
	var accepted, rejected int
	for entry, err := range p.Logs {
		if err != nil {
			rejected++
			continue
		}
		log.Printf("ingested: %+v", entry)
		accepted++
	}
	c.JSON(http.StatusOK, IngestReport{Accepted: accepted, Rejected: rejected})
}

func frames(c *router.Context) {
	mp := c.MultipartMixed(http.StatusOK)
	defer mp.Close()
	for i := 0; i < 3; i++ {
		if err := mp.Part(
			textproto.MIMEHeader{
				"Content-Type":  {"application/octet-stream"},
				"X-Frame-Index": {fmt.Sprintf("%d", i)},
				"X-Captured-At": {time.Now().UTC().Format(time.RFC3339Nano)},
			},
			io.LimitReader(strings.NewReader(strings.Repeat("X", 64)), 64),
		); err != nil {
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func main() {
	r := router.New()

	gen := openapi.NewGenerator(openapi.Info{
		Title:   "minmux Showcase",
		Summary: "Browseable surface area for minmux's OAS 3.2 generator",
		Version: "0.1.0",
		Description: "A broad surface area for the Scalar UI to render — " +
			"params, formats, ProblemDetails, every OAS 3.2 security " +
			"scheme (incl. deviceAuthorization), and SSE / JSONL / " +
			"multipart/mixed streams.",
		TermsOfService: "https://example.com/tos",
		Contact: &openapi.Contact{
			Name:  "minmux maintainers",
			URL:   "https://github.com/joakimcarlsson/minmux/issues",
			Email: "noreply@example.com",
		},
		License: &openapi.License{
			Name:       "MIT",
			Identifier: "MIT",
		},
	})

	// Document-level Tag Objects — OAS 3.2 promotes tags to a structured
	// navigation element. Parent builds nested groups; Kind hints to the
	// renderer how the tag is meant to surface (here, "nav" = navigation).
	gen.Tags = []*openapi.Tag{
		{
			Name:        "Catalog",
			Summary:     "Resources",
			Description: "Domain resources exposed by the API.",
			Kind:        "nav",
		},
		{
			Name:        "Pets",
			Parent:      "Catalog",
			Summary:     "Pets",
			Description: "CRUD over pets, including a deprecated legacy listing.",
		},
		{
			Name:        "Users",
			Parent:      "Catalog",
			Summary:     "Users",
			Description: "Authenticated user profile and password rotation.",
		},
		{
			Name:        "Realtime",
			Summary:     "Realtime",
			Description: "Streaming endpoints.",
			Kind:        "nav",
		},
		{
			Name:        "Streams",
			Parent:      "Realtime",
			Summary:     "SSE / JSONL / multipart",
			Description: "Server-sent events, JSON Lines, and multipart/mixed streams.",
			ExternalDocs: &openapi.ExternalDocs{
				URL:         "https://github.com/joakimcarlsson/minmux/blob/main/www/docs/streaming.md",
				Description: "Streaming reference",
			},
		},
		{
			Name:        "Auth",
			Summary:     "Auth flows",
			Description: "Authentication and authorization flows (incl. RFC 8628 device authorization).",
		},
		{
			Name:        "Meta",
			Summary:     "Health & metadata",
			Description: "Liveness probes and other observability endpoints.",
		},
	}
	gen.ExternalDocs = &openapi.ExternalDocs{
		URL:         "https://github.com/joakimcarlsson/minmux",
		Description: "minmux on GitHub",
	}

	// Document-level servers — Scalar (and any OAS UI) renders a base-URL
	// selector and pre-fills "Try it" requests against the chosen entry.
	gen.Servers = []*openapi.Server{
		{
			URL:         "http://localhost:8080",
			Description: "This local instance",
		},
		{
			URL:         "https://api.example.com/v1",
			Description: "Production",
		},
		{
			URL:         "https://{environment}.example.com/v1",
			Description: "Non-production tiers",
			Variables: map[string]*openapi.ServerVariable{
				"environment": {
					Default:     "staging",
					Enum:        []string{"staging", "dev"},
					Description: "Deployment tier",
				},
			},
		},
	}

	gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
		"basicAuth":  openapi.BasicAuth("Username + password over HTTPS"),
		"bearerAuth": openapi.BearerAuth("JWT", "Bearer JWT issued by the IdP"),
		"apiKeyAuth": openapi.APIKey(
			"header",
			"X-Api-Key",
			"Long-lived service key",
		),
		"mtls": openapi.MutualTLS(
			"Client cert must chain to example.com CA",
		),
		"oidc": openapi.OpenIDConnect(
			"https://issuer.example/.well-known/openid-configuration",
			"OpenID Connect Discovery",
		),
		"petstoreOAuth": openapi.OAuth2Scheme(&openapi.OAuthFlows{
			AuthorizationCode: &openapi.OAuthFlow{
				AuthorizationURL: "https://auth.example/oauth/authorize",
				TokenURL:         "https://auth.example/oauth/token",
				RefreshURL:       "https://auth.example/oauth/refresh",
				Scopes: map[string]string{
					"read:pets":  "List and read pets",
					"write:pets": "Create, update, and delete pets",
				},
			},
			ClientCredentials: &openapi.OAuthFlow{
				TokenURL: "https://auth.example/oauth/token",
				Scopes: map[string]string{
					"admin:pets": "Administer pets (service-to-service)",
				},
			},
			DeviceAuthorization: &openapi.OAuthFlow{
				DeviceAuthorizationURL: "https://auth.example/oauth/device_authorization",
				TokenURL:               "https://auth.example/oauth/token",
				Scopes: map[string]string{
					"read:pets": "List and read pets (from a TV or CLI)",
				},
			},
		}, "Pet store user OAuth2 grants"),
	}
	gen.SecuritySchemes["petstoreOAuth"].OAuth2MetadataURL =
		"https://auth.example/.well-known/oauth-authorization-server"

	// Document default: every operation requires a bearer JWT unless it
	// overrides via NoSecurity or its own Security calls.
	gen.Security = []openapi.SecurityRequirement{{"bearerAuth": {}}}

	r.Get("/health", health,
		openapi.Summary("Liveness probe"),
		openapi.Tags("Meta"),
		openapi.ReturnsBody[map[string]string](http.StatusOK, "Healthy"),
		openapi.NoSecurity(),
	)

	users := r.Group("/users", openapi.Tags("Users"))

	users.Get("/me", me,
		openapi.Summary("Current user profile"),
		openapi.Description("Inherits the document-level bearerAuth default."),
		openapi.ReturnsBody[User](http.StatusOK, "Profile"),
	)

	users.Post("/me/password", changePassword,
		openapi.Summary("Change password"),
		openapi.Description(
			"Body uses `format:\"password\"` on both fields — Scalar masks "+
				"them in the request editor.",
		),
		openapi.Returns(http.StatusNoContent, "Password rotated"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest,
			"Password does not meet policy",
		),
	)

	pets := r.Group("/pets",
		openapi.Tags("Pets"),
		openapi.Security("petstoreOAuth", "read:pets"),
	)

	pets.Get(
		"",
		listPets,
		// Auto-derived id would be "getPets"; override for a nicer
		// generated client method name.
		openapi.OperationID("listPets"),
		openapi.Summary("List pets"),
		openapi.Description(
			"Filter by ?tag=, page with ?limit=, propagate X-Trace-Id.",
		),
		openapi.ReturnsBody[[]Pet](http.StatusOK, "Pets"),
		openapi.OptionalSecurity(),
	)

	pets.Get("/{id}", getPet,
		openapi.Summary("Get a pet by id"),
		openapi.ReturnsBody[Pet](
			http.StatusOK,
			"Pet",
			openapi.WithHeader(
				"ETag",
				"Opaque revision marker for conditional GETs",
			),
			openapi.WithHeader(
				"Cache-Control",
				"Cache hints (e.g. max-age=60)",
			),
		),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound,
			"Pet not found",
		),
	)

	pets.Post("", createPet,
		openapi.Summary("Create a pet"),
		openapi.ReturnsBody[Pet](http.StatusCreated, "Pet created",
			openapi.WithHeader("Location", "URL of the newly created pet"),
		),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest,
			"Invalid body",
		),
		openapi.Security("petstoreOAuth", "write:pets"),
	)

	pets.Get("/legacy", listPets,
		openapi.Summary("List pets (legacy)"),
		openapi.Description(
			"Old listing endpoint kept for back-compat. Use GET /pets instead.",
		),
		openapi.Deprecated(),
		openapi.ReturnsBody[[]Pet](http.StatusOK, "Pets",
			openapi.WithHeader("Retry-After",
				"Seconds before retrying when rate-limited",
				openapi.WithHeaderSchema(&openapi.Schema{
					Type: "integer", Format: "int32",
				}),
			),
		),
		openapi.OptionalSecurity(),
	)

	pets.Delete(
		"/{id}",
		deletePet,
		openapi.Summary("Delete a pet (admin only)"),
		openapi.Description(
			"AND: must present a valid client cert AND an admin OAuth2 token.",
		),
		openapi.Returns(http.StatusNoContent, "Deleted"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound,
			"Pet not found",
		),
		openapi.SecurityAll(openapi.SecurityRequirement{
			"mtls":          {},
			"petstoreOAuth": {"admin:pets"},
		}),
	)

	r.Post("/auth/device", deviceLogin,
		openapi.ExternalDocsLink(
			"https://datatracker.ietf.org/doc/html/rfc8628",
			"RFC 8628 — OAuth 2.0 Device Authorization Grant",
		),
		openapi.Summary("Begin a device authorization flow"),
		openapi.Description(
			"Demonstrates the OAS 3.2 deviceAuthorization OAuth2 flow as "+
				"an alternative to the document-level bearer requirement.",
		),
		openapi.Tags("Auth"),
		openapi.ReturnsBody[map[string]string](http.StatusOK, "Flow started"),
		openapi.Security("petstoreOAuth", "read:pets"),
		openapi.Security("apiKeyAuth"),
	)

	streams := r.Group(
		"/streams",
		openapi.Tags("Streams"),
		openapi.NoSecurity(),
	)

	streams.Get("/tokens", tokens,
		openapi.Summary("Streaming AI tokens via SSE"),
		openapi.Description(
			"Emits one Server-Sent Event per token. Each event's data field "+
				"carries a JSON-encoded Token; the spec annotates it with "+
				"contentMediaType + contentSchema per OAS 3.2 §4.14.4.",
		),
		openapi.SSEStream[Token](http.StatusOK, "Token stream"),
	)

	streams.Get("/logs.jsonl", logsJSONL,
		openapi.Summary("Streaming logs as JSON Lines"),
		openapi.StreamsBody[LogEntry](
			http.StatusOK,
			"Newline-delimited log entries",
			"application/jsonl",
			"application/x-ndjson",
		),
	)

	streams.Post("/ingest", ingest,
		openapi.Summary("Ingest a stream of log records"),
		openapi.Description(
			"Accepts JSONL or NDJSON. The handler ranges over iter.Seq2 "+
				"so records are consumed as they arrive on the wire.",
		),
		openapi.ReturnsBody[IngestReport](http.StatusOK, "Ingest summary"),
	)

	streams.Get("/frames", frames,
		openapi.Summary("Streaming frames via multipart/mixed"),
		openapi.Description(
			"Each frame is one application/octet-stream part with "+
				"X-Frame-Index and X-Captured-At metadata headers. "+
				"Matches OAS 3.2 §4.15.4.8 (Streaming Multipart).",
		),
		openapi.MultipartMixedStream[FrameMeta](
			http.StatusOK,
			"Streaming frames",
			openapi.WithItemContentType("application/octet-stream"),
		),
	)

	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))
	r.HandleFunc(http.MethodGet, "/docs", scalar.HandlerWith(scalar.Config{
		SpecURL: "/openapi.json",
		Title:   "minmux Showcase — Reference",
	}))

	addr := ":8080"
	fmt.Println("listening on", addr, "(spec at /openapi.json, docs at /docs)")
	log.Fatal(http.ListenAndServe(addr, r))
}
