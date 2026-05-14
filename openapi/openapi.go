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

// Generator builds OpenAPI 3.1 specs from a router by reflecting on the
// registered endpoints' parameter and result types.
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
		OpenAPI:           "3.1.0",
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
	op := &Operation{
		Tags:        ep.GetTags(),
		Summary:     ep.GetSummary(),
		Description: ep.GetDescription(),
		Responses:   b.buildResponses(ep),
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
				Schema:   b.schema(f.Type),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("query"); ok {
			params = append(params, &Parameter{
				Name:   v,
				In:     "query",
				Schema: b.schema(f.Type),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("header"); ok {
			params = append(params, &Parameter{
				Name:   v,
				In:     "header",
				Schema: b.schema(f.Type),
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

func (b *schemaBuilder) buildResponses(
	ep *router.Endpoint,
) map[string]*Response {
	schema, status := b.resultSchema(ep.ResultType)
	resp := &Response{Description: http.StatusText(status)}
	if schema != nil {
		resp.Content = map[string]*MediaType{
			"application/json": {Schema: schema},
		}
	}
	return map[string]*Response{strconv.Itoa(status): resp}
}

// resultSchema unwraps minmux result wrappers (Ok[T], Created[T], NoContent,
// Redirect) to their inner schema and matching HTTP status.
func (b *schemaBuilder) resultSchema(t reflect.Type) (*Schema, int) {
	if t == nil {
		return nil, http.StatusOK
	}
	name := t.Name()
	switch {
	case name == "NoContent":
		return nil, http.StatusNoContent
	case strings.HasPrefix(name, "Ok["):
		if t.NumField() > 0 {
			return b.schema(t.Field(0).Type), http.StatusOK
		}
		return nil, http.StatusOK
	case strings.HasPrefix(name, "Created["):
		if t.NumField() > 0 {
			return b.schema(t.Field(0).Type), http.StatusCreated
		}
		return nil, http.StatusCreated
	case name == "Redirect":
		return nil, http.StatusSeeOther
	}
	return b.schema(t), http.StatusOK
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
	if t.Kind() == reflect.Struct && t.Name() != "" &&
		!isInternalResultType(t) {
		name := t.Name()
		if _, exists := b.components[name]; !exists {
			b.components[name] = &Schema{} // placeholder for recursive types
			b.components[name] = b.structSchema(t)
		}
		return &Schema{Ref: "#/components/schemas/" + name}
	}
	return b.inline(t)
}

// inline returns the inline schema for t without hoisting.
func (b *schemaBuilder) inline(t reflect.Type) *Schema {
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: "array", Items: b.schema(t.Elem())}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: b.schema(t.Elem())}
	case reflect.Struct:
		return b.structSchema(t)
	}
	return &Schema{Type: "object"}
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
		props[name] = b.schema(f.Type)
	}
	return &Schema{Type: "object", Properties: props}
}

// isInternalResultType reports whether t is one of minmux's response wrappers,
// which should never be hoisted into components.
func isInternalResultType(t reflect.Type) bool {
	n := t.Name()
	return strings.HasPrefix(n, "Ok[") ||
		strings.HasPrefix(n, "Created[") ||
		n == "NoContent" ||
		n == "Redirect"
}
