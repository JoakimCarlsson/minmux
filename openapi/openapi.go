package openapi

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/router"
)

var (
	timeType          = reflect.TypeOf(time.Time{})
	readerType        = reflect.TypeFor[io.Reader]()
	bytesType         = reflect.TypeFor[[]byte]()
	fileHeaderPtrType = reflect.TypeFor[*multipart.FileHeader]()
	fileHeaderSlcType = reflect.TypeFor[[]*multipart.FileHeader]()
	errorType         = reflect.TypeFor[error]()
	sseEventType      = reflect.TypeFor[router.SSEEvent]()
)

// Info is the OpenAPI document info block. OAS 3.2 fields:
//
//   - Title, Version are required.
//   - Summary is a short one-line label (3.1+); Description is long-form
//     CommonMark.
//   - TermsOfService is a URL to the API's terms of service.
//   - Contact and License describe the API publisher and licensing.
//
// Renderers like Scalar surface this block prominently above the
// operations list.
type Info struct {
	Title          string   `json:"title"`
	Version        string   `json:"version"`
	Summary        string   `json:"summary,omitempty"`
	Description    string   `json:"description,omitempty"`
	TermsOfService string   `json:"termsOfService,omitempty"`
	Contact        *Contact `json:"contact,omitempty"`
	License        *License `json:"license,omitempty"`
}

// Contact is the API publisher's contact info.
type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// License describes the API's licensing. OAS 3.1+ allows Identifier
// (SPDX expression) as an alternative to URL; the spec mandates they
// are mutually exclusive — set only one.
type License struct {
	Name       string `json:"name"`
	Identifier string `json:"identifier,omitempty"`
	URL        string `json:"url,omitempty"`
}

// Generator builds OpenAPI 3.2 specs from a router by reading the openapi
// options attached to each endpoint. Responses are taken purely from
// explicit Returns[T] declarations; the handler signature provides no
// implicit success response.
//
// SecuritySchemes registers reusable auth definitions under
// components.securitySchemes. Security sets the document-level default
// Security Requirement list; individual operations may override it via
// the Security / NoSecurity / OptionalSecurity options.
//
// Servers populates the document-level servers array. Renderers like
// Scalar and Swagger UI use this to offer a base-URL selector and to
// pre-fill the host portion of "Try it out" requests.
//
// Tags populates the document-level Tag Object array, giving operation
// tags real metadata (summary, description, parent → nested groups,
// externalDocs). Operations still reference tags by string name via
// openapi.Tags(...); the Document.Tags entries supply the structured
// rendering metadata for each name. ExternalDocs is the document-level
// "see also" link.
type Generator struct {
	Info            Info
	Servers         []*Server
	Tags            []*Tag
	ExternalDocs    *ExternalDocs
	SecuritySchemes map[string]*SecurityScheme
	Security        []SecurityRequirement
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
		Servers:           g.Servers,
		Paths:             paths,
		Security:          g.Security,
		Tags:              g.Tags,
		ExternalDocs:      g.ExternalDocs,
	}
	if len(b.components) > 0 || len(g.SecuritySchemes) > 0 {
		doc.Components = &Components{}
		if len(b.components) > 0 {
			doc.Components.Schemas = b.components
		}
		if len(g.SecuritySchemes) > 0 {
			doc.Components.SecuritySchemes = g.SecuritySchemes
		}
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
	opID := m.OperationID
	if opID == "" {
		opID = deriveOperationID(ep.Method, ep.Path)
	}
	op := &Operation{
		Tags:         m.Tags,
		Summary:      m.Summary,
		Description:  m.Description,
		ExternalDocs: m.ExternalDocs,
		OperationID:  opID,
		Deprecated:   m.Deprecated,
		Responses:    b.buildResponses(m),
		Security:     operationSecurity(m),
	}
	if ep.ParamType != nil {
		op.Parameters, op.RequestBody = b.buildParams(ep.ParamType)
	}
	op.Parameters = fillMissingPathParams(ep.Path, op.Parameters)
	return op
}

// fillMissingPathParams ensures every "{name}" segment in the route path
// has a corresponding Parameter Object, as required by OAS 3.2 §4.4.1.1.
// Names already declared via the handler's Params struct are left alone;
// the rest get a generic required string parameter so the generated spec
// is always conformant, even when a handler ignores a path variable.
// Go 1.22 ServeMux wildcard segments ({name...}) are emitted under their
// base name; the "{$}" end-of-path anchor is skipped (not a parameter).
func fillMissingPathParams(path string, params []*Parameter) []*Parameter {
	declared := map[string]bool{}
	for _, p := range params {
		if p.In == "path" {
			declared[p.Name] = true
		}
	}
	for _, name := range pathTemplateNames(path) {
		if declared[name] {
			continue
		}
		params = append(params, &Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
		declared[name] = true
	}
	return params
}

// deriveOperationID builds a default operationId from an HTTP method and
// a route path. Static segments are appended in camelCase; templated
// segments like "{id}" become "ById". Wildcard "{name...}" is treated as
// "{name}"; the "{$}" end-of-path anchor is skipped.
//
//	GET  /pets             -> getPets
//	GET  /pets/{id}        -> getPetsById
//	POST /users/me/password -> postUsersMePassword
//	GET  /files/{path...}  -> getFilesByPath
//
// Uniqueness across the document is guaranteed by method + path being
// unique on a router.
func deriveOperationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, raw := range strings.Split(path, "/") {
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
			name := raw[1 : len(raw)-1]
			if name == "$" {
				continue
			}
			name = strings.TrimSuffix(name, "...")
			if name == "" {
				continue
			}
			b.WriteString("By")
			b.WriteString(capitalize(name))
			continue
		}
		b.WriteString(capitalize(raw))
	}
	return b.String()
}

// capitalize uppercases the first rune of s and leaves the rest alone.
// Empty input returns empty. Used by deriveOperationID so segments like
// "pets" become "Pets" without lowercasing camelCase segments the user
// may have used in their route ("petStore" stays "PetStore").
func capitalize(s string) string {
	s = sanitizeSegment(s)
	if s == "" {
		return s
	}
	first := s[0]
	if first >= 'a' && first <= 'z' {
		first -= 'a' - 'A'
	}
	return string(first) + s[1:]
}

// sanitizeSegment normalizes a single path segment into a safe identifier
// fragment: non-alphanumeric runes become word breaks that capitalize the
// following character, and the rest is dropped. So "logs.jsonl" becomes
// "logsJsonl" — operationIds remain valid identifiers in generated
// clients even when paths contain dots, dashes, or other separators.
func sanitizeSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	upperNext := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9':
			if upperNext && c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			b.WriteByte(c)
			upperNext = false
		default:
			upperNext = b.Len() > 0
		}
	}
	return b.String()
}

// pathTemplateNames returns the ordered list of variable names extracted
// from a Go 1.22 ServeMux pattern path. The "{name...}" wildcard form is
// reduced to "name"; the "{$}" anchor is skipped.
func pathTemplateNames(path string) []string {
	var out []string
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			continue
		}
		end := strings.IndexByte(path[i+1:], '}')
		if end < 0 {
			return out
		}
		name := path[i+1 : i+1+end]
		i += end + 1
		if name == "$" {
			continue
		}
		name = strings.TrimSuffix(name, "...")
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// operationSecurity translates the accumulated endpoint security meta into
// the Operation.Security pointer. nil means "inherit document default",
// a non-nil empty slice emits "security": [] and clears the inherited
// default, and a non-empty slice lists the alternative requirements.
func operationSecurity(m *endpointMeta) *[]SecurityRequirement {
	if len(m.Security) > 0 {
		s := m.Security
		return &s
	}
	if m.SecurityOverride {
		s := []SecurityRequirement{}
		return &s
	}
	return nil
}

// formProperty captures a single form: / file: field for assembling a
// multipart or x-www-form-urlencoded request body schema.
type formProperty struct {
	name     string
	schema   *Schema
	encoding *Encoding
	required bool
}

func (b *schemaBuilder) buildParams(
	t reflect.Type,
) (params []*Parameter, body *RequestBody) {
	var (
		formProps []formProperty
		fileProps []formProperty
		bodyField *reflect.StructField
	)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if v, ok := f.Tag.Lookup("path"); ok {
			params = append(params, &Parameter{
				Name:       v,
				In:         "path",
				Required:   true,
				Deprecated: fieldDeprecated(f),
				Schema:     applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("query"); ok {
			params = append(params, &Parameter{
				Name:       v,
				In:         "query",
				Required:   isRequiredKind(f.Type),
				Deprecated: fieldDeprecated(f),
				Schema:     applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("header"); ok {
			params = append(params, &Parameter{
				Name:       v,
				In:         "header",
				Required:   isRequiredKind(f.Type),
				Deprecated: fieldDeprecated(f),
				Schema:     applyFieldFormat(b.schema(f.Type), f),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("form"); ok {
			formProps = append(formProps, formProperty{
				name:     v,
				schema:   applyFieldFormat(b.schema(f.Type), f),
				required: isRequiredKind(f.Type),
			})
			continue
		}
		if v, ok := f.Tag.Lookup("file"); ok {
			fileProps = append(fileProps, formProperty{
				name:     v,
				schema:   fileSchema(f.Type),
				encoding: encodingFromTag(f),
				required: f.Type == fileHeaderPtrType,
			})
			continue
		}
		if _, ok := f.Tag.Lookup("body"); ok {
			f := f
			bodyField = &f
		}
	}

	body = b.buildRequestBody(formProps, fileProps, bodyField)
	return params, body
}

// buildRequestBody picks the right content type based on the tag mix and
// builds the corresponding RequestBody. The precedence is:
//
//   - any file: field   -> multipart/form-data (with form fields as text parts)
//   - else any form:    -> application/x-www-form-urlencoded
//   - else body: stream -> application/octet-stream (or contentType tag values)
//   - else body: struct -> application/json (existing behavior)
//   - else              -> no request body
func (b *schemaBuilder) buildRequestBody(
	formProps, fileProps []formProperty,
	bodyField *reflect.StructField,
) *RequestBody {
	if len(fileProps) > 0 {
		return formRequestBody(
			"multipart/form-data", append(formProps, fileProps...),
		)
	}
	if len(formProps) > 0 {
		return formRequestBody(
			"application/x-www-form-urlencoded", formProps,
		)
	}
	if bodyField == nil {
		return nil
	}
	return b.bodyRequestBody(*bodyField)
}

func formRequestBody(contentType string, props []formProperty) *RequestBody {
	schema := &Schema{
		Type:       "object",
		Properties: map[string]*Schema{},
	}
	encodings := map[string]*Encoding{}
	for _, p := range props {
		schema.Properties[p.name] = p.schema
		if p.required {
			schema.Required = append(schema.Required, p.name)
		}
		if p.encoding != nil {
			encodings[p.name] = p.encoding
		}
	}
	mt := &MediaType{Schema: schema}
	if len(encodings) > 0 {
		mt.Encoding = encodings
	}
	return &RequestBody{
		Required: true,
		Content:  map[string]*MediaType{contentType: mt},
	}
}

func (b *schemaBuilder) bodyRequestBody(f reflect.StructField) *RequestBody {
	if f.Type == readerType || f.Type == bytesType {
		types := splitContentTypes(f.Tag.Get("contentType"))
		if len(types) == 0 {
			types = []string{"application/octet-stream"}
		}
		content := map[string]*MediaType{}
		for _, ct := range types {
			content[ct] = &MediaType{
				Schema: &Schema{Type: "string", Format: "binary"},
			}
		}
		return &RequestBody{Required: true, Content: content}
	}
	if item, ok := iterSeq2ItemType(f.Type); ok {
		return b.iterRequestBody(
			item,
			splitContentTypes(f.Tag.Get("contentType")),
		)
	}
	return &RequestBody{
		Required: true,
		Content: map[string]*MediaType{
			"application/json": {Schema: b.schema(f.Type)},
		},
	}
}

// iterRequestBody builds a streaming request body for iter.Seq2[T, error]
// fields. SSE input emits the canonical SSE event schema, multipart/mixed
// input emits an empty media type (parts have heterogeneous types), and
// sequential JSON inputs emit itemSchema: schema(T).
func (b *schemaBuilder) iterRequestBody(
	item reflect.Type,
	contentTypes []string,
) *RequestBody {
	if len(contentTypes) == 0 {
		contentTypes = []string{"application/jsonl"}
	}
	content := map[string]*MediaType{}
	for _, ct := range contentTypes {
		switch normalizeMediaType(ct) {
		case "text/event-stream":
			var t reflect.Type
			if item != sseEventType {
				t = item
			}
			content[ct] = &MediaType{ItemSchema: b.sseEventSchema(t)}
		case "multipart/mixed", "multipart/byteranges":
			content[ct] = &MediaType{}
		default:
			content[ct] = &MediaType{ItemSchema: b.schema(item)}
		}
	}
	return &RequestBody{Required: true, Content: content}
}

// iterSeq2ItemType returns T and true when t is iter.Seq2[T, error] (which
// in Go's type system is a func(yield func(T, error) bool) value). Kept in
// sync with the router-side detector in router/bind.go.
func iterSeq2ItemType(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() != reflect.Func {
		return nil, false
	}
	if t.NumIn() != 1 || t.NumOut() != 0 {
		return nil, false
	}
	yield := t.In(0)
	if yield.Kind() != reflect.Func {
		return nil, false
	}
	if yield.NumIn() != 2 || yield.NumOut() != 1 {
		return nil, false
	}
	if yield.Out(0).Kind() != reflect.Bool {
		return nil, false
	}
	if yield.In(1) != errorType {
		return nil, false
	}
	return yield.In(0), true
}

// normalizeMediaType strips parameters and lowercases a media type.
func normalizeMediaType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// fileSchema returns the JSON Schema fragment for a `file:` field: a
// binary string for single files, an array of binary strings for repeated
// uploads.
func fileSchema(t reflect.Type) *Schema {
	if t == fileHeaderSlcType {
		return &Schema{
			Type:  "array",
			Items: &Schema{Type: "string", Format: "binary"},
		}
	}
	return &Schema{Type: "string", Format: "binary"}
}

// encodingFromTag pulls a `contentType:"..."` tag off a file field and
// renders it as an OAS Encoding Object. Returns nil when the tag is
// absent so we can omit the encoding map entirely.
func encodingFromTag(f reflect.StructField) *Encoding {
	raw := f.Tag.Get("contentType")
	types := splitContentTypes(raw)
	if len(types) == 0 {
		return nil
	}
	return &Encoding{ContentType: strings.Join(types, ", ")}
}

// isRequiredKind decides whether a form: field belongs in the object
// schema's required array. Pointers and slices are optional; everything
// else (scalars, named structs) is required.
func isRequiredKind(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice:
		return false
	}
	return true
}

func splitContentTypes(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildResponses turns the explicit Returns[T] / StreamsBody[T] / SSEStream[T]
// / MultipartMixedStream[T] declarations into the responses block. Endpoints
// with no declared responses get a "default" placeholder so the document
// remains a valid OpenAPI spec.
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
		out[strconv.Itoa(decl.Status)] = b.responseFromDecl(decl, desc)
	}
	return out
}

func (b *schemaBuilder) responseFromDecl(
	decl ResponseDecl,
	desc string,
) *Response {
	r := &Response{Description: desc, Headers: decl.Headers}
	switch decl.StreamKind {
	case streamSequential:
		r.Content = map[string]*MediaType{}
		var itemSchema *Schema
		if decl.ItemType != nil {
			itemSchema = b.schema(decl.ItemType)
		}
		for _, ct := range decl.ContentTypes {
			r.Content[ct] = &MediaType{ItemSchema: itemSchema}
		}
	case streamSSE:
		r.Content = map[string]*MediaType{
			"text/event-stream": {
				ItemSchema: b.sseEventSchema(decl.ItemType),
			},
		}
	case streamMultipartMixed:
		mt := &MediaType{}
		if decl.ItemType != nil {
			mt.ItemSchema = b.schema(decl.ItemType)
		} else {
			mt.ItemSchema = &Schema{}
		}
		if decl.ItemEncoding != nil {
			mt.ItemEncoding = decl.ItemEncoding
		} else {
			mt.ItemEncoding = &Encoding{ContentType: "application/octet-stream"}
		}
		if len(decl.PrefixParts) > 0 {
			mt.PrefixEncoding = append([]*Encoding(nil), decl.PrefixParts...)
		}
		r.Content = map[string]*MediaType{"multipart/mixed": mt}
	default:
		if decl.BodyType != nil {
			r.Content = map[string]*MediaType{
				"application/json": {Schema: b.schema(decl.BodyType)},
			}
		}
	}
	return r
}

// sseEventSchema builds the canonical text/event-stream event schema from
// OAS 3.2 §4.14.4. When itemType is non-nil, the data property is annotated
// with contentMediaType: application/json + contentSchema: schema(itemType)
// to indicate the data field carries a JSON payload of that shape.
func (b *schemaBuilder) sseEventSchema(itemType reflect.Type) *Schema {
	data := &Schema{Type: "string"}
	if itemType != nil {
		data.ContentMediaType = "application/json"
		data.ContentSchema = b.schema(itemType)
	}
	return &Schema{
		Type:     "object",
		Required: []string{"data"},
		Properties: map[string]*Schema{
			"data":  data,
			"event": {Type: "string"},
			"id":    {Type: "string"},
			"retry": {Type: "integer", Minimum: zero()},
		},
	}
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

// applyFieldFormat applies a struct field's `format:"..."` tag and any
// schema-constraint tags (minimum, maximum, minLength, maxLength,
// pattern, enum, default) to its schema, overriding auto-inferred
// values. The format tag is a passthrough for OAS-registered formats
// (email, password, uuid, uri, ...). Constraint values are parsed in
// terms of the field's Go type — `minimum:"0"` on an int field becomes
// a numeric minimum, `enum:"a,b,c"` on a string field becomes a string
// enum, and so on.
func applyFieldFormat(s *Schema, f reflect.StructField) *Schema {
	if v, ok := f.Tag.Lookup("format"); ok && v != "" {
		s.Format = v
	}
	applyFieldConstraints(s, f)
	return s
}

// applyFieldConstraints reads the schema-constraint tags off f and
// writes them onto s. Values are parsed in terms of the scalar element
// type underlying f (slice elements, pointer elements). Unparseable
// values silently fall through — the spec stays valid, the constraint
// just doesn't apply.
func applyFieldConstraints(s *Schema, f reflect.StructField) {
	t := scalarKind(f.Type)

	if v, ok := f.Tag.Lookup("minimum"); ok && v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			s.Minimum = &n
		}
	}
	if v, ok := f.Tag.Lookup("maximum"); ok && v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			s.Maximum = &n
		}
	}
	if v, ok := f.Tag.Lookup("minLength"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			s.MinLength = &n
		}
	}
	if v, ok := f.Tag.Lookup("maxLength"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			s.MaxLength = &n
		}
	}
	if v, ok := f.Tag.Lookup("pattern"); ok && v != "" {
		s.Pattern = v
	}
	if v, ok := f.Tag.Lookup("enum"); ok && v != "" {
		parts := strings.Split(v, ",")
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			out = append(out, coerceScalar(p, t))
		}
		if len(out) > 0 {
			s.Enum = out
		}
	}
	if v, ok := f.Tag.Lookup("default"); ok {
		s.Default = coerceScalar(v, t)
	}
}

// scalarKind returns the underlying scalar Kind of t — peeling off
// pointers and slice/array element types so that `*int` and `[]int` both
// report Int. Used by tag parsers to decide how to coerce string tag
// values to typed enum/default entries.
func scalarKind(t reflect.Type) reflect.Kind {
	for {
		switch t.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			t = t.Elem()
			continue
		}
		return t.Kind()
	}
}

// coerceScalar turns a struct-tag string into the typed Go value matching
// kind, so JSON marshaling emits e.g. `"enum":[1,2,3]` for an int field
// rather than `"enum":["1","2","3"]`. Unparseable input falls back to the
// raw string — the spec stays valid even if the value type drifts.
func coerceScalar(v string, kind reflect.Kind) any {
	switch kind {
	case reflect.Bool:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	case reflect.Float32, reflect.Float64:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return v
}

// fieldDeprecated reports whether a parameter field carries
// `deprecated:"true"` (or "1"). The flag becomes Parameter.Deprecated in
// the emitted spec.
func fieldDeprecated(f reflect.StructField) bool {
	v, ok := f.Tag.Lookup("deprecated")
	if !ok {
		return false
	}
	return v == "true" || v == "1"
}
