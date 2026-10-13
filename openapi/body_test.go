package openapi

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type rawJSONParams struct {
	Body []byte `body:"" contentType:"application/json" desc:"The event as sent."`
}

type rawJSONStreamParams struct {
	Body io.Reader `body:"" contentType:"application/json, application/cloudevents+json"`
}

type Envelope struct {
	ID      string          `json:"id"      desc:"The event id."`
	Payload json.RawMessage `json:"payload" desc:"The event's data."`
}

// requireRef fails the test unless content carries a media type for ct
// whose schema references the named component.
func requireRef(
	t *testing.T,
	content map[string]*MediaType,
	ct, component string,
) {
	t.Helper()
	mt, ok := content[ct]
	if !ok {
		t.Fatalf("no %q content, got %v", ct, contentKeys(content))
	}
	want := "#/components/schemas/" + component
	if mt.Schema == nil || mt.Schema.Ref != want {
		t.Errorf("%s schema: want $ref %s, got %+v", ct, want, mt.Schema)
	}
}

// TestSpec_AcceptsBodyDocumentsRawBytes checks that a []byte body is
// documented with the declared type, keeping its content type, its
// description and the required flag.
func TestSpec_AcceptsBodyDocumentsRawBytes(t *testing.T) {
	r := router.New()
	r.Post("/events", noopP[rawJSONParams], AcceptsBody[Envelope]())
	op := operation(t, r, "/events", "POST")

	rb := op.RequestBody
	if rb == nil {
		t.Fatal("missing requestBody")
	}
	if len(rb.Content) != 1 {
		t.Errorf("content: want only application/json, got %v",
			contentKeys(rb.Content))
	}
	requireRef(t, rb.Content, "application/json", "Envelope")
	if !rb.Required {
		t.Error("requestBody should stay required")
	}
	if rb.Description != "The event as sent." {
		t.Errorf("description: %q", rb.Description)
	}
}

// TestSpec_AcceptsBodyHoistsTheType checks that the declared type lands in
// components with its fields and their descriptions.
func TestSpec_AcceptsBodyHoistsTheType(t *testing.T) {
	r := router.New()
	r.Post("/events", noopP[rawJSONParams], AcceptsBody[Envelope]())
	spec := NewGenerator(Info{}).Spec(r)

	if spec.Components == nil {
		t.Fatal("missing components")
	}
	env, ok := spec.Components.Schemas["Envelope"]
	if !ok {
		t.Fatalf("Envelope not in components: %v",
			slices.Sorted(maps.Keys(spec.Components.Schemas)))
	}
	if env.Properties["id"] == nil ||
		env.Properties["id"].Description != "The event id." {
		t.Errorf("id property: %+v", env.Properties["id"])
	}
	if env.Properties["payload"] == nil ||
		env.Properties["payload"].Description != "The event's data." {
		t.Errorf("payload property: %+v", env.Properties["payload"])
	}
}

// TestSpec_AcceptsBodyCoversEveryContentType checks that each content type
// of an io.Reader body gets the declared schema.
func TestSpec_AcceptsBodyCoversEveryContentType(t *testing.T) {
	r := router.New()
	r.Post("/events", noopP[rawJSONStreamParams], AcceptsBody[Envelope]())
	op := operation(t, r, "/events", "POST")

	if len(op.RequestBody.Content) != 2 {
		t.Errorf("content: want 2 types, got %v",
			contentKeys(op.RequestBody.Content))
	}
	requireRef(t, op.RequestBody.Content, "application/json", "Envelope")
	requireRef(
		t,
		op.RequestBody.Content,
		"application/cloudevents+json",
		"Envelope",
	)
}

// TestSpec_AcceptsBodyWithoutBodyField checks that a route with no body
// field is documented as taking the type as a required JSON body.
func TestSpec_AcceptsBodyWithoutBodyField(t *testing.T) {
	r := router.New()
	r.Post("/events", noop, AcceptsBody[Envelope]())
	op := operation(t, r, "/events", "POST")

	if op.RequestBody == nil {
		t.Fatal("missing requestBody")
	}
	if !op.RequestBody.Required {
		t.Error("requestBody should be required")
	}
	if len(op.RequestBody.Content) != 1 {
		t.Errorf("content: want only application/json, got %v",
			contentKeys(op.RequestBody.Content))
	}
	requireRef(t, op.RequestBody.Content, "application/json", "Envelope")
}

// TestSpec_AcceptsBodyReplacesStructBody checks that the declared type
// wins over the type of a struct body field.
func TestSpec_AcceptsBodyReplacesStructBody(t *testing.T) {
	r := router.New()
	r.Post("/items", noopP[bodyParams], AcceptsBody[Envelope]())
	op := operation(t, r, "/items", "POST")

	requireRef(t, op.RequestBody.Content, "application/json", "Envelope")
}

// TestSpec_AcceptsBodyIsPerRoute checks that the declaration on one route
// leaves another route with the same params alone.
func TestSpec_AcceptsBodyIsPerRoute(t *testing.T) {
	r := router.New()
	r.Post("/typed", noopP[rawJSONParams], AcceptsBody[Envelope]())
	r.Post("/raw", noopP[rawJSONParams])
	op := operation(t, r, "/raw", "POST")

	s := op.RequestBody.Content["application/json"].Schema
	if s == nil || s.Type != "string" || s.Format != "binary" {
		t.Errorf("raw route: want binary string, got %+v", s)
	}
}

// TestSpec_RawBodyCarriesDescription checks that a raw body without
// AcceptsBody stays a binary string and still carries its desc tag.
func TestSpec_RawBodyCarriesDescription(t *testing.T) {
	r := router.New()
	r.Post("/events", noopP[rawJSONParams])
	op := operation(t, r, "/events", "POST")

	s := op.RequestBody.Content["application/json"].Schema
	if s == nil || s.Type != "string" || s.Format != "binary" {
		t.Errorf("want binary string, got %+v", s)
	}
	if op.RequestBody.Description != "The event as sent." {
		t.Errorf("description: %q", op.RequestBody.Description)
	}
}

// TestSpec_StructBodyCarriesDescription checks that a struct body's desc
// tag becomes the requestBody description.
func TestSpec_StructBodyCarriesDescription(t *testing.T) {
	type describedBody struct {
		Body User `body:"" desc:"The user to create."`
	}
	r := router.New()
	r.Post("/users", noopP[describedBody])
	op := operation(t, r, "/users", "POST")

	if op.RequestBody.Description != "The user to create." {
		t.Errorf("description: %q", op.RequestBody.Description)
	}
}

// TestSpec_RequestBodyDescriptionOmittedWhenUnset checks that a body
// without a desc tag emits no description key.
func TestSpec_RequestBodyDescriptionOmittedWhenUnset(t *testing.T) {
	r := router.New()
	r.Post("/items", noopP[bodyParams])
	op := operation(t, r, "/items", "POST")

	data, err := json.Marshal(op.RequestBody)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"description"`) {
		t.Errorf("unset description should be omitted: %s", data)
	}
}

// TestAcceptsBody_HandlerStillReceivesRawBytes serves a documented route
// and checks that the handler gets the body byte for byte, whitespace
// included, while the served spec documents the declared type.
func TestAcceptsBody_HandlerStillReceivesRawBytes(t *testing.T) {
	sent := "{\n  \"id\": \"e1\",\n  \"payload\": {\"a\": 1}\n}\n"
	var got []byte
	r := router.New()
	r.Post("/events", func(c *router.Context, p rawJSONParams) {
		got = p.Body
		c.NoContent()
	}, AcceptsBody[Envelope]())
	r.HandleFunc(http.MethodGet, "/openapi.json",
		NewGenerator(Info{Title: "T", Version: "1"}).Handler(r))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/events",
		"application/json",
		strings.NewReader(sent),
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if string(got) != sent {
		t.Errorf("body: want %q, got %q", sent, got)
	}

	resp, err = http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Schema map[string]any `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	schema := doc.Paths["/events"].Post.RequestBody.
		Content["application/json"].Schema
	if schema["$ref"] != "#/components/schemas/Envelope" {
		t.Errorf("served schema: %v", schema)
	}
	if _, ok := doc.Components.Schemas["Envelope"]; !ok {
		t.Error("served components miss Envelope")
	}
}

// TestSchema_RawMessageIsAnyValue checks that json.RawMessage, which holds
// any JSON value, is documented by the empty schema rather than as an
// array of bytes.
func TestSchema_RawMessageIsAnyValue(t *testing.T) {
	b := newSchemaBuilder()
	for _, typ := range []any{json.RawMessage{}, &json.RawMessage{}} {
		data, err := json.Marshal(b.schema(reflect.TypeOf(typ)))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "{}" {
			t.Errorf("%T: want {}, got %s", typ, data)
		}
	}

	r := router.New()
	r.Post("/events", noop, AcceptsBody[Envelope]())
	spec := NewGenerator(Info{}).Spec(r)
	data, err := json.Marshal(
		spec.Components.Schemas["Envelope"].Properties["payload"],
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"description":"The event's data."}` {
		t.Errorf("payload property: %s", data)
	}
}
