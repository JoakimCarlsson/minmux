package openapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

func TestInlineSchema_Scalars(t *testing.T) {
	b := newSchemaBuilder()
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"string", "", "string"},
		{"bool", true, "boolean"},
		{"int", int(0), "integer"},
		{"int8", int8(0), "integer"},
		{"int16", int16(0), "integer"},
		{"int32", int32(0), "integer"},
		{"int64", int64(0), "integer"},
		{"uint", uint(0), "integer"},
		{"uint8", uint8(0), "integer"},
		{"uint16", uint16(0), "integer"},
		{"uint32", uint32(0), "integer"},
		{"uint64", uint64(0), "integer"},
		{"float32", float32(0), "number"},
		{"float64", float64(0), "number"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := b.schema(reflect.TypeOf(c.val))
			if got.Type != c.want {
				t.Errorf("type: want %q, got %q", c.want, got.Type)
			}
		})
	}
}

func TestInlineSchema_NumericFormats(t *testing.T) {
	cases := []struct {
		name       string
		val        any
		wantType   string
		wantFormat string
		wantMin    bool
	}{
		{"int8", int8(0), "integer", "", false},
		{"int16", int16(0), "integer", "", false},
		{"int32", int32(0), "integer", "int32", false},
		{"int64", int64(0), "integer", "int64", false},
		{"int", int(0), "integer", "int64", false},
		{"uint8", uint8(0), "integer", "", true},
		{"uint16", uint16(0), "integer", "", true},
		{"uint32", uint32(0), "integer", "int32", true},
		{"uint64", uint64(0), "integer", "int64", true},
		{"uint", uint(0), "integer", "int64", true},
		{"float32", float32(0), "number", "float", false},
		{"float64", float64(0), "number", "double", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := newSchemaBuilder().schema(reflect.TypeOf(c.val))
			if got.Type != c.wantType {
				t.Errorf("type: want %q, got %q", c.wantType, got.Type)
			}
			if got.Format != c.wantFormat {
				t.Errorf("format: want %q, got %q", c.wantFormat, got.Format)
			}
			if c.wantMin {
				if got.Minimum == nil || *got.Minimum != 0 {
					t.Errorf("minimum: want 0, got %+v", got.Minimum)
				}
			} else if got.Minimum != nil {
				t.Errorf("minimum: want unset, got %+v", *got.Minimum)
			}
		})
	}
}

func TestSchema_MinimumMarshalsAsZero(t *testing.T) {
	got := newSchemaBuilder().schema(reflect.TypeOf(uint32(0)))
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"minimum":0`) {
		t.Errorf("minimum should marshal as 0 (not 0.0), got %s", raw)
	}
}

func TestInlineSchema_Time(t *testing.T) {
	got := newSchemaBuilder().schema(reflect.TypeOf(time.Time{}))
	if got.Type != "string" || got.Format != "date-time" {
		t.Errorf("time.Time: want string/date-time, got %+v", got)
	}
}

func TestInlineSchema_PointerUnwraps(t *testing.T) {
	var s string
	got := newSchemaBuilder().schema(reflect.TypeOf(&s))
	if got.Type != "string" {
		t.Errorf("*string: want string, got %+v", got)
	}
}

func TestInlineSchema_SliceOfScalar(t *testing.T) {
	got := newSchemaBuilder().schema(reflect.TypeOf([]int{}))
	if got.Type != "array" {
		t.Fatalf("type: want array, got %+v", got)
	}
	if got.Items == nil || got.Items.Type != "integer" {
		t.Errorf("items: want integer, got %+v", got.Items)
	}
}

func TestInlineSchema_Map(t *testing.T) {
	got := newSchemaBuilder().schema(reflect.TypeOf(map[string]int{}))
	if got.Type != "object" {
		t.Errorf("type: want object, got %+v", got)
	}
	if got.AdditionalProperties == nil ||
		got.AdditionalProperties.Type != "integer" {
		t.Errorf(
			"additionalProperties: want integer, got %+v",
			got.AdditionalProperties,
		)
	}
}

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Skip  string `json:"-"`
	priv  string //nolint:unused
	Untag bool
}

type Author struct {
	Name string `json:"name"`
}

type Book struct {
	ID      int       `json:"id"`
	Title   string    `json:"title"`
	Author  Author    `json:"author"`
	Created time.Time `json:"created_at"`
}

type Node struct {
	ID       int     `json:"id"`
	Children []*Node `json:"children"`
}

type ErrorModel struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func TestSchema_NamedStructProducesRef(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf(User{}))
	if got.Ref != "#/components/schemas/User" {
		t.Errorf("ref: %q", got.Ref)
	}
	user := b.components["User"]
	if user == nil {
		t.Fatalf("User not registered in components: %v", b.components)
	}
	if user.Properties["id"].Type != "integer" {
		t.Errorf("User.id: %+v", user.Properties["id"])
	}
	if _, ok := user.Properties["Skip"]; ok {
		t.Errorf("json:\"-\" field should be excluded")
	}
	if _, ok := user.Properties["priv"]; ok {
		t.Errorf("unexported field should be excluded")
	}
	if _, ok := user.Properties["Untag"]; !ok {
		t.Errorf("untagged exported field should use field name")
	}
}

func TestSchema_NestedStructHoists(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf(Book{}))
	if got.Ref != "#/components/schemas/Book" {
		t.Errorf("Book ref: %q", got.Ref)
	}
	if _, ok := b.components["Author"]; !ok {
		t.Errorf("Author should be hoisted, components: %v", b.components)
	}
	book := b.components["Book"]
	if book.Properties["author"].Ref != "#/components/schemas/Author" {
		t.Errorf(
			"Book.author: want $ref to Author, got %+v",
			book.Properties["author"],
		)
	}
	created := book.Properties["created_at"]
	if created.Type != "string" || created.Format != "date-time" {
		t.Errorf(
			"Book.created_at: want string/date-time inline, got %+v",
			created,
		)
	}
}

func TestSchema_SliceOfStructItemsAreRef(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf([]User{}))
	if got.Type != "array" {
		t.Fatalf("type: want array, got %+v", got)
	}
	if got.Items.Ref != "#/components/schemas/User" {
		t.Errorf("items: want $ref to User, got %+v", got.Items)
	}
}

func TestSchema_RecursiveStructDoesNotInfiniteLoop(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf(Node{}))
	if got.Ref != "#/components/schemas/Node" {
		t.Errorf("Node ref: %q", got.Ref)
	}
	node := b.components["Node"]
	children := node.Properties["children"]
	if children.Type != "array" {
		t.Fatalf("children: want array, got %+v", children)
	}
	if children.Items.Ref != "#/components/schemas/Node" {
		t.Errorf("children.items: want $ref to Node, got %+v", children.Items)
	}
}

func TestSchema_TimeIsNotHoisted(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf(time.Time{}))
	if got.Type != "string" {
		t.Errorf("time.Time inline: %+v", got)
	}
	if _, ok := b.components["Time"]; ok {
		t.Errorf("time.Time should not be in components")
	}
}

func TestSpec_TopLevelFields(t *testing.T) {
	r := router.New()
	spec := NewGenerator(
		Info{Title: "T", Version: "1.0", Description: "d"},
	).Spec(r)

	if spec.OpenAPI != "3.2.0" {
		t.Errorf("openapi: %q", spec.OpenAPI)
	}
	if spec.JSONSchemaDialect != "https://spec.openapis.org/oas/3.1/dialect/base" {
		t.Errorf("jsonSchemaDialect: %q", spec.JSONSchemaDialect)
	}
	if spec.Info.Title != "T" || spec.Info.Version != "1.0" ||
		spec.Info.Description != "d" {
		t.Errorf("info: %+v", spec.Info)
	}
}

type pathParams struct {
	ID int `path:"id"`
}

type queryParams struct {
	Limit int   `query:"limit"`
	Done  *bool `query:"done"`
}

type headerParams struct {
	TraceID string `header:"X-Trace-Id"`
}

type bodyParams struct {
	Body User `body:""`
}

type formattedQueryParams struct {
	Email string `query:"email" format:"email"`
	ID    int32  `query:"id"    format:"int64"`
}

type formattedBody struct {
	Email    string `json:"email"    format:"email"`
	Password string `json:"password" format:"password"`
	UUID     string `json:"uuid"     format:"uuid"`
	Untagged string `json:"untagged"`
}

func noop(c *router.Context)              {}
func noopP[P any](c *router.Context, p P) {}
func noopUser(c *router.Context)          {}

func TestSpec_PathParam(t *testing.T) {
	r := router.New()
	r.Get("/items/{id}", noopP[pathParams])
	op := operation(t, r, "/items/{id}", "GET")
	if len(op.Parameters) != 1 {
		t.Fatalf("want 1 param, got %d", len(op.Parameters))
	}
	p := op.Parameters[0]
	if p.Name != "id" || p.In != "path" || !p.Required {
		t.Errorf("path param: %+v", p)
	}
	if p.Schema.Type != "integer" {
		t.Errorf("path schema: %+v", p.Schema)
	}
}

func TestSpec_QueryParams(t *testing.T) {
	r := router.New()
	r.Get("/items", noopP[queryParams])
	op := operation(t, r, "/items", "GET")
	if len(op.Parameters) != 2 {
		t.Fatalf("want 2 params, got %d", len(op.Parameters))
	}
	byName := map[string]*Parameter{}
	for _, p := range op.Parameters {
		byName[p.Name] = p
	}
	if byName["limit"].In != "query" {
		t.Errorf("limit in: %q", byName["limit"].In)
	}
	if byName["limit"].Required {
		t.Errorf("query params should not be required by default")
	}
	if byName["done"].Schema.Type != "boolean" {
		t.Errorf("done schema: %+v", byName["done"].Schema)
	}
}

func TestSpec_HeaderParam(t *testing.T) {
	r := router.New()
	r.Get("/items", noopP[headerParams])
	op := operation(t, r, "/items", "GET")
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "X-Trace-Id" ||
		op.Parameters[0].In != "header" {
		t.Errorf("header param: %+v", op.Parameters)
	}
}

func TestSpec_FormatTagOnQueryParam(t *testing.T) {
	r := router.New()
	r.Get("/items", noopP[formattedQueryParams])
	op := operation(t, r, "/items", "GET")
	byName := map[string]*Parameter{}
	for _, p := range op.Parameters {
		byName[p.Name] = p
	}
	if got := byName["email"].Schema; got.Type != "string" ||
		got.Format != "email" {
		t.Errorf("email param: want string/email, got %+v", got)
	}
	if got := byName["id"].Schema; got.Type != "integer" ||
		got.Format != "int64" {
		t.Errorf("id param: tag should override auto int32, got %+v", got)
	}
}

func TestSchema_FormatTagOnStructFields(t *testing.T) {
	b := newSchemaBuilder()
	got := b.schema(reflect.TypeOf(formattedBody{}))
	if got.Ref == "" {
		t.Fatalf("formattedBody should be hoisted, got %+v", got)
	}
	body := b.components["formattedBody"]
	if body == nil {
		t.Fatalf("formattedBody not in components: %v", b.components)
	}
	cases := []struct {
		field      string
		wantFormat string
	}{
		{"email", "email"},
		{"password", "password"},
		{"uuid", "uuid"},
		{"untagged", ""},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			s := body.Properties[c.field]
			if s == nil {
				t.Fatalf("missing property %q", c.field)
			}
			if s.Type != "string" {
				t.Errorf("type: want string, got %q", s.Type)
			}
			if s.Format != c.wantFormat {
				t.Errorf("format: want %q, got %q", c.wantFormat, s.Format)
			}
		})
	}
}

func TestSpec_RequestBodyIsRef(t *testing.T) {
	r := router.New()
	r.Post("/items", noopP[bodyParams])
	op := operation(t, r, "/items", "POST")
	if op.RequestBody == nil {
		t.Fatal("missing requestBody")
	}
	if !op.RequestBody.Required {
		t.Errorf("body required: %v", op.RequestBody.Required)
	}
	schema := op.RequestBody.Content["application/json"].Schema
	if schema.Ref != "#/components/schemas/User" {
		t.Errorf("body schema: want $ref to User, got %+v", schema)
	}
}

func TestSpec_NoResponsesYieldsDefault(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)
	op := operation(t, r, "/u", "GET")
	if _, ok := op.Responses["default"]; !ok {
		t.Errorf(
			"expected default response when none declared, got %v",
			op.Responses,
		)
	}
	if _, ok := op.Responses["200"]; ok {
		t.Errorf("expected no implicit 200, got %v", op.Responses)
	}
}

func TestSpec_ExplicitReturns(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		ReturnsBody[User](http.StatusOK, "User"),
		ReturnsBody[router.ProblemDetails](http.StatusNotFound, "Not found"),
	)
	op := operation(t, r, "/u", "GET")

	r200 := op.Responses["200"]
	if r200 == nil {
		t.Fatalf("missing 200, responses: %v", op.Responses)
	}
	if r200.Description != "User" {
		t.Errorf("200 description: %q", r200.Description)
	}
	if r200.Content["application/json"].Schema.Ref != "#/components/schemas/User" {
		t.Errorf("200 schema: %+v", r200.Content["application/json"].Schema)
	}

	r404 := op.Responses["404"]
	if r404 == nil {
		t.Fatalf("missing 404, responses: %v", op.Responses)
	}
	if r404.Content["application/json"].Schema.Ref != "#/components/schemas/ProblemDetails" {
		t.Errorf("404 schema: %+v", r404.Content["application/json"].Schema)
	}

	if _, ok := op.Responses["default"]; ok {
		t.Errorf("default should not appear when explicit responses declared")
	}
}

func TestSpec_ReturnsArray(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		ReturnsBody[[]User](http.StatusOK, "List of users"),
	)
	op := operation(t, r, "/u", "GET")
	schema := op.Responses["200"].Content["application/json"].Schema
	if schema.Type != "array" {
		t.Fatalf("schema: want array, got %+v", schema)
	}
	if schema.Items.Ref != "#/components/schemas/User" {
		t.Errorf("items: want $ref to User, got %+v", schema.Items)
	}
}

func TestSpec_MetadataOptions(t *testing.T) {
	r := router.New()
	r.Get("/u", noop,
		Tags("Users"),
		Summary("Get user"),
		Description("Fetch a user by ID"),
	)
	op := operation(t, r, "/u", "GET")
	if op.Summary != "Get user" {
		t.Errorf("summary: %q", op.Summary)
	}
	if op.Description != "Fetch a user by ID" {
		t.Errorf("description: %q", op.Description)
	}
	if len(op.Tags) != 1 || op.Tags[0] != "Users" {
		t.Errorf("tags: %v", op.Tags)
	}
}

func TestSpec_GroupTagsCascade(t *testing.T) {
	r := router.New()
	g := r.Group("/api/v1", Tags("V1"))
	g.Get("/u", noop, Tags("Users"))

	op := operation(t, r, "/api/v1/u", "GET")
	if len(op.Tags) != 2 || op.Tags[0] != "V1" || op.Tags[1] != "Users" {
		t.Errorf("tags cascade: want [V1 Users], got %v", op.Tags)
	}
}

func TestSpec_GroupNestingCascades(t *testing.T) {
	r := router.New()
	v1 := r.Group("/api/v1", Tags("V1"))
	users := v1.Group("/users", Tags("Users"))
	users.Get("/{id}", noopP[pathParams])

	op := operation(t, r, "/api/v1/users/{id}", "GET")
	if len(op.Tags) != 2 || op.Tags[0] != "V1" || op.Tags[1] != "Users" {
		t.Errorf("nested tags: want [V1 Users], got %v", op.Tags)
	}
}

func TestSpec_MultipleMethodsSamePath(t *testing.T) {
	r := router.New()
	r.Get("/u", noop)
	r.Post("/u", noopP[bodyParams])
	spec := NewGenerator(Info{}).Spec(r)
	item := spec.Paths["/u"]
	if item.Get == nil {
		t.Errorf("missing GET")
	}
	if item.Post == nil {
		t.Errorf("missing POST")
	}
}

func TestSpec_ComponentsAccumulate(t *testing.T) {
	r := router.New()
	r.Get("/u", noop, ReturnsBody[User](http.StatusOK, ""))
	r.Get("/b", noop, ReturnsBody[Book](http.StatusOK, ""))

	spec := NewGenerator(Info{}).Spec(r)
	if spec.Components == nil {
		t.Fatal("missing components")
	}
	for _, name := range []string{"User", "Book", "Author"} {
		if _, ok := spec.Components.Schemas[name]; !ok {
			t.Errorf(
				"missing component %s, schemas: %v",
				name,
				spec.Components.Schemas,
			)
		}
	}
}

func TestSpec_EmptyComponentsNotEmitted(t *testing.T) {
	r := router.New()
	r.Get("/p", noopP[pathParams])

	spec := NewGenerator(Info{}).Spec(r)
	if spec.Components != nil {
		t.Errorf(
			"components should be omitted when no schemas hoisted, got %+v",
			spec.Components,
		)
	}
}

func TestSpec_JSONFieldOrder(t *testing.T) {
	r := router.New()
	r.Get("/u/{id}", noopP[pathParams], ReturnsBody[User](http.StatusOK, ""))

	raw, err := json.Marshal(
		NewGenerator(Info{Title: "T", Version: "1"}).Spec(r),
	)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	idxOpenAPI := strings.Index(s, `"openapi"`)
	idxInfo := strings.Index(s, `"info"`)
	idxPaths := strings.Index(s, `"paths"`)
	idxComponents := strings.Index(s, `"components"`)
	if !(idxOpenAPI < idxInfo && idxInfo < idxPaths && idxPaths < idxComponents) {
		t.Errorf(
			"field order wrong: openapi=%d info=%d paths=%d components=%d in %s",
			idxOpenAPI,
			idxInfo,
			idxPaths,
			idxComponents,
			s,
		)
	}
}

func operation(t *testing.T, r *router.Router, path, method string) *Operation {
	t.Helper()
	spec := NewGenerator(Info{}).Spec(r)
	item, ok := spec.Paths[path]
	if !ok {
		t.Fatalf("path %q not in spec, paths: %v", path, spec.Paths)
	}
	var op *Operation
	switch method {
	case "GET":
		op = item.Get
	case "PUT":
		op = item.Put
	case "POST":
		op = item.Post
	case "DELETE":
		op = item.Delete
	case "PATCH":
		op = item.Patch
	}
	if op == nil {
		t.Fatalf("method %q missing on %q", method, path)
	}
	return op
}
