package openapi

import (
	"context"
	"encoding/json"
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

func TestInlineSchema_DoublePointerUnwraps(t *testing.T) {
	var s string
	p := &s
	got := newSchemaBuilder().schema(reflect.TypeOf(&p))
	if got.Type != "string" {
		t.Errorf("**string: want string, got %+v", got)
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

func TestResultSchema_BareValue(t *testing.T) {
	b := newSchemaBuilder()
	schema, status := b.resultSchema(reflect.TypeOf(User{}))
	if status != 200 {
		t.Errorf("status: want 200, got %d", status)
	}
	if schema.Ref != "#/components/schemas/User" {
		t.Errorf("schema: want $ref to User, got %+v", schema)
	}
}

func TestResultSchema_Ok(t *testing.T) {
	b := newSchemaBuilder()
	schema, status := b.resultSchema(reflect.TypeOf(router.Ok[User]{}))
	if status != 200 {
		t.Errorf("status: want 200, got %d", status)
	}
	if schema.Ref != "#/components/schemas/User" {
		t.Errorf("Ok schema: want $ref to User, got %+v", schema)
	}
}

func TestResultSchema_Created(t *testing.T) {
	b := newSchemaBuilder()
	schema, status := b.resultSchema(reflect.TypeOf(router.Created[User]{}))
	if status != 201 {
		t.Errorf("status: want 201, got %d", status)
	}
	if schema.Ref != "#/components/schemas/User" {
		t.Errorf("Created schema: want $ref to User, got %+v", schema)
	}
}

func TestResultSchema_NoContent(t *testing.T) {
	b := newSchemaBuilder()
	schema, status := b.resultSchema(reflect.TypeOf(router.NoContent{}))
	if status != 204 {
		t.Errorf("status: want 204, got %d", status)
	}
	if schema != nil {
		t.Errorf("NoContent schema: want nil, got %+v", schema)
	}
	if _, ok := b.components["NoContent"]; ok {
		t.Errorf("NoContent must not be hoisted into components")
	}
}

func TestResultSchema_Redirect(t *testing.T) {
	_, status := newSchemaBuilder().resultSchema(reflect.TypeOf(router.Redirect{}))
	if status != 303 {
		t.Errorf("Redirect status: want 303, got %d", status)
	}
}

func TestSpec_TopLevelFields(t *testing.T) {
	r := router.New()
	spec := NewGenerator(
		Info{Title: "T", Version: "1.0", Description: "d"},
	).Spec(r)

	if spec.OpenAPI != "3.1.0" {
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

func TestSpec_PathParam(t *testing.T) {
	r := router.New()
	r.Get("/items/{id}", func(_ context.Context, _ pathParams) (User, error) {
		return User{}, nil
	})
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
	r.Get("/items", func(_ context.Context, _ queryParams) ([]User, error) {
		return nil, nil
	})
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
	r.Get("/items", func(_ context.Context, _ headerParams) (User, error) {
		return User{}, nil
	})
	op := operation(t, r, "/items", "GET")
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "X-Trace-Id" ||
		op.Parameters[0].In != "header" {
		t.Errorf("header param: %+v", op.Parameters)
	}
}

func TestSpec_RequestBodyIsRef(t *testing.T) {
	r := router.New()
	r.Post(
		"/items",
		func(_ context.Context, _ bodyParams) (router.Created[User], error) {
			return router.Created[User]{}, nil
		},
	)
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

func TestSpec_ResponsesAreRef(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) (User, error) { return User{}, nil })
	op := operation(t, r, "/u", "GET")
	schema := op.Responses["200"].Content["application/json"].Schema
	if schema.Ref != "#/components/schemas/User" {
		t.Errorf("response schema: want $ref to User, got %+v", schema)
	}
}

func TestSpec_ListResponseHasItemsRef(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) ([]User, error) { return nil, nil })
	op := operation(t, r, "/u", "GET")
	schema := op.Responses["200"].Content["application/json"].Schema
	if schema.Type != "array" {
		t.Fatalf("schema: want array, got %+v", schema)
	}
	if schema.Items.Ref != "#/components/schemas/User" {
		t.Errorf("items: want $ref to User, got %+v", schema.Items)
	}
}

func TestSpec_NoContentHasNoBody(t *testing.T) {
	r := router.New()
	r.Delete("/u", func(_ context.Context) (router.NoContent, error) {
		return router.NoContent{}, nil
	})
	op := operation(t, r, "/u", "DELETE")
	r204 := op.Responses["204"]
	if r204.Content != nil {
		t.Errorf("NoContent should have no content, got %+v", r204.Content)
	}
}

func TestSpec_Metadata(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) (User, error) { return User{}, nil }).
		Tags("Users").
		Summary("Get user").
		Description("Fetch a user by ID")
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
	g := r.Group("/api/v1").Tags("V1")
	g.Get("/u", func(_ context.Context) (User, error) { return User{}, nil }).
		Tags("Users")

	op := operation(t, r, "/api/v1/u", "GET")
	if len(op.Tags) != 2 || op.Tags[0] != "V1" || op.Tags[1] != "Users" {
		t.Errorf("tags cascade: want [V1 Users], got %v", op.Tags)
	}
}

func TestSpec_MultipleMethodsSamePath(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) (User, error) { return User{}, nil })
	r.Post(
		"/u",
		func(_ context.Context, _ bodyParams) (router.Created[User], error) {
			return router.Created[User]{}, nil
		},
	)
	spec := NewGenerator(Info{}).Spec(r)
	item := spec.Paths["/u"]
	if item.Get == nil {
		t.Errorf("missing GET")
	}
	if item.Post == nil {
		t.Errorf("missing POST")
	}
}

func TestSpec_ComponentsSchemasAccumulate(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) (User, error) { return User{}, nil })
	r.Get("/b", func(_ context.Context) (Book, error) { return Book{}, nil })

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
	r.Get(
		"/p",
		func(_ context.Context, _ pathParams) (string, error) { return "", nil },
	)

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
	r.Get("/u/{id}", func(_ context.Context, _ pathParams) (User, error) {
		return User{}, nil
	})

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

func TestSpec_SchemaFieldOrder(t *testing.T) {
	r := router.New()
	r.Get("/u", func(_ context.Context) (User, error) { return User{}, nil })
	raw, err := json.Marshal(NewGenerator(Info{}).Spec(r))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if i := strings.Index(s, `"$ref":"#/components/schemas/User"`); i < 0 {
		t.Fatalf("missing User $ref in: %s", s)
	}
	user := strings.Index(s, `"User":{`)
	if user < 0 {
		t.Fatalf("missing User definition: %s", s)
	}
	rest := s[user:]
	typeIdx := strings.Index(rest, `"type"`)
	propsIdx := strings.Index(rest, `"properties"`)
	if !(typeIdx >= 0 && propsIdx >= 0 && typeIdx < propsIdx) {
		t.Errorf(
			"schema order: type=%d properties=%d in %s",
			typeIdx,
			propsIdx,
			rest,
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
