package openapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

var timeType = reflect.TypeOf(time.Time{})

// Info is the OpenAPI document info block.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// Generator builds OpenAPI 3.2 specs from a router by reading the openapi
// options attached to each endpoint. Responses are taken purely from
// explicit Returns[T] declarations; the handler signature provides no
// implicit success response.
type Generator struct {
	Info Info
}

// NewGenerator constructs a Generator.
func NewGenerator(info Info) *Generator {
	return &Generator{Info: info}
}

// Spec returns the OpenAPI document for a router.
func (g *Generator) Spec(r *router.Router) *Document {
	b := newSchemaBuilder()
	paths := map[string]*PathItem{}
	for _, ep := range r.Endpoints() {
		op := b.buildOperation(ep)
		item, ok := paths[ep.Path]
		if !ok {
			item = &PathItem{}
			paths[ep.Path] = item
		}
		setOperation(item, ep.Method, op)
	}
	doc := &Document{
		OpenAPI:           "3.2.0",
		Info:              g.Info,
		JSONSchemaDialect: "https://spec.openapis.org/oas/3.1/dialect/base",
		Paths:             paths,
	}
	if len(b.components) > 0 {
		doc.Components = &Components{Schemas: b.components}
	}
	return doc
}

// Handler returns an http.HandlerFunc that serves the spec as JSON.
func (g *Generator) Handler(r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g.Spec(r))
	}
}

func setOperation(p *PathItem, method string, op *Operation) {
	switch method {
	case http.MethodGet:
		p.Get = op
	case http.MethodPut:
		p.Put = op
	case http.MethodPost:
		p.Post = op
	case http.MethodDelete:
		p.Delete = op
	case http.MethodPatch:
		p.Patch = op
	case http.MethodHead:
		p.Head = op
	case http.MethodOptions:
		p.Options = op
	case http.MethodTrace:
		p.Trace = op
	}
}

// schemaBuilder accumulates reusable schema definitions during a single Spec
// generation pass. Named user struct types are hoisted into components and
// referenced by $ref; everything else is inlined.
type schemaBuilder struct {
	components map[string]*Schema
}

func newSchemaBuilder() *schemaBuilder {
	return &schemaBuilder{components: map[string]*Schema{}}
}

func (b *schemaBuilder) buildOperation(ep *router.Endpoint) *Operation {
	m := readMeta(ep)
	op := &Operation{
		Tags:        m.Tags,
		Summary:     m.Summary,
		Description: m.Description,
		Responses:   b.buildResponses(m),
	}
	if ep.ParamType != nil {
		op.Parameters, op.RequestBody = b.buildParams(ep.ParamType)
	}
	return op
}

func (b *schemaBuilder) buildParams(
	t reflect.Type,
) (params []*Parameter, body *RequestBody) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if v, ok := f.Tag.Lookup("path"); ok {
			params = append(params, &Parameter{
				Name:     v,
				In:       "path",
				Required: true,
				Schema:   applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("query"); ok {
			params = append(params, &Parameter{
				Name:   v,
				In:     "query",
				Schema: applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("header"); ok {
			params = append(params, &Parameter{
				Name:   v,
				In:     "header",
				Schema: applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if _, ok := f.Tag.Lookup("body"); ok {
			body = &RequestBody{
				Required: true,
				Content: map[string]*MediaType{
					"application/json": {Schema: b.schema(f.Type)},
				},
			}
		}
	}
	return params, body
}

// buildResponses turns the explicit Returns[T] declarations into the
// responses block. Endpoints with no declared responses get a "default"
// placeholder so the document remains a valid OpenAPI spec.
func (b *schemaBuilder) buildResponses(m *endpointMeta) map[string]*Response {
	if len(m.Responses) == 0 {
		return map[string]*Response{
			"default": {Description: "Default response"},
		}
	}
	out := map[string]*Response{}
	for _, decl := range m.Responses {
		desc := decl.Description
		if desc == "" {
			desc = http.StatusText(decl.Status)
		}
		out[strconv.Itoa(decl.Status)] = b.responseFromType(decl.BodyType, desc)
	}
	return out
}

func (b *schemaBuilder) responseFromType(
	t reflect.Type,
	desc string,
) *Response {
	r := &Response{Description: desc}
	if t != nil {
		r.Content = map[string]*MediaType{
			"application/json": {Schema: b.schema(t)},
		}
	}
	return r
}

// schema returns the JSON Schema for t. Named user struct types are hoisted
// into components and returned as a $ref; everything else is inlined.
func (b *schemaBuilder) schema(t reflect.Type) *Schema {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}
	}
	if t.Kind() == reflect.Struct && t.Name() != "" {
		name := t.Name()
		if _, exists := b.components[name]; !exists {
			b.components[name] = &Schema{} // placeholder for recursive types
			b.components[name] = b.structSchema(t)
		}
		return &Schema{Ref: "#/components/schemas/" + name}
	}
	return b.inline(t)
}

func (b *schemaBuilder) inline(t reflect.Type) *Schema {
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int8, reflect.Int16:
		return &Schema{Type: "integer"}
	case reflect.Int32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Int, reflect.Int64:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Uint8, reflect.Uint16:
		return &Schema{Type: "integer", Minimum: zero()}
	case reflect.Uint32:
		return &Schema{Type: "integer", Format: "int32", Minimum: zero()}
	case reflect.Uint, reflect.Uint64:
		return &Schema{Type: "integer", Format: "int64", Minimum: zero()}
	case reflect.Float32:
		return &Schema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &Schema{Type: "number", Format: "double"}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: "array", Items: b.schema(t.Elem())}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: b.schema(t.Elem())}
	case reflect.Struct:
		return b.structSchema(t)
	}
	return &Schema{Type: "object"}
}

func zero() *float64 {
	z := 0.0
	return &z
}

func (b *schemaBuilder) structSchema(t reflect.Type) *Schema {
	props := map[string]*Schema{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			if comma := strings.Index(tag, ","); comma >= 0 {
				tag = tag[:comma]
			}
			if tag == "-" {
				continue
			}
			if tag != "" {
				name = tag
			}
		}
		props[name] = applyFieldFormat(b.schema(f.Type), f)
	}
	return &Schema{Type: "object", Properties: props}
}

// applyFieldFormat applies a struct field's `format:"..."` tag to its
// schema, overriding any auto-inferred format. The tag is a passthrough
// for OAS-registered formats (email, password, uuid, uri, ...) that
// cannot be inferred from the Go type alone.
func applyFieldFormat(s *Schema, f reflect.StructField) *Schema {
	if v, ok := f.Tag.Lookup("format"); ok && v != "" {
		s.Format = v
	}
	return s
}
