package clientgen

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/joakimcarlsson/minmux/openapi"
)

// bodyKind classifies how an operation's request body is encoded.
type bodyKind int

const (
	bodyNone bodyKind = iota
	bodyJSON
	bodyBinary
	bodyURLEncoded
	bodyMultipart
	bodyJSONStream
)

// streamKind classifies how a streaming response is decoded.
type streamKind int

const (
	streamNone streamKind = iota
	streamJSON
	streamSSE
	streamMultipart
)

// operation is one generated client method.
type operation struct {
	Name        string
	ParamsType  string
	Method      string
	Path        string
	PathParts   []pathPart
	Summary     string
	Description string
	Deprecated  bool
	Params      []opParam
	Body        *bodySpec
	Success     responseSpec
	Extra2xx    []int
	Errors      []errorSpec
	Default     *errorSpec
	Stream      *streamSpec
	Accept      string
}

// HasInput reports whether the operation takes a params struct at all.
func (o *operation) HasInput() bool { return o.ParamsType != "" }

// pathPart is one piece of a route path: either a literal run of
// characters or a substitution of a path parameter.
type pathPart struct {
	Literal  string
	Field    string
	Wildcard bool
}

// opParam is a single path, query, or header parameter.
type opParam struct {
	Field      string
	Wire       string
	In         string
	Type       string
	Base       string
	Required   bool
	IsSlice    bool
	Doc        string
	Deprecated bool
}

// bodySpec describes an operation's request body.
type bodySpec struct {
	Kind        bodyKind
	Type        string
	ContentType string
	ItemType    string
	FramerRS    bool
	Fields      []formField
	Files       []formField
}

// formField is one property of a urlencoded or multipart request body.
type formField struct {
	Field    string
	Wire     string
	Type     string
	Base     string
	Required bool
	IsSlice  bool
}

// streamSpec describes a streaming success response. ItemType is set only
// for sequential-JSON streams; SSE and multipart streams yield the
// runtime's SSEEvent and Part types respectively.
type streamSpec struct {
	Kind     streamKind
	Status   int
	ItemType string
}

// responseSpec is the operation's success response.
type responseSpec struct {
	Status int
	Type   string
}

// errorSpec is a declared non-success status and the type its body
// decodes into. Type is empty when the response declares no body.
type errorSpec struct {
	Status int
	Type   string
}

// buildOperations walks the document and produces the client methods, in a
// stable order: by path, then by the canonical HTTP method order.
func (g *generator) buildOperations() ([]*operation, error) {
	var ops []*operation
	for _, path := range slices.Sorted(maps.Keys(g.cfg.Spec.Paths)) {
		item := g.cfg.Spec.Paths[path]
		for _, entry := range methodsOf(item) {
			op, err := g.buildOperation(path, entry.method, entry.op)
			if err != nil {
				return nil, fmt.Errorf(
					"%s %s: %w", entry.method, path, err,
				)
			}
			ops = append(ops, op)
		}
	}
	sort.SliceStable(ops, func(i, j int) bool {
		return ops[i].Name < ops[j].Name
	})
	return ops, nil
}

type methodEntry struct {
	method string
	op     *openapi.Operation
}

// methodsOf returns the operations on a path item in a fixed order so that
// generation never depends on struct field iteration incidentals.
func methodsOf(item *openapi.PathItem) []methodEntry {
	all := []methodEntry{
		{http.MethodGet, item.Get},
		{http.MethodHead, item.Head},
		{http.MethodPost, item.Post},
		{http.MethodPut, item.Put},
		{http.MethodPatch, item.Patch},
		{http.MethodDelete, item.Delete},
		{http.MethodOptions, item.Options},
		{http.MethodTrace, item.Trace},
	}
	out := make([]methodEntry, 0, len(all))
	for _, e := range all {
		if e.op != nil {
			out = append(out, e)
		}
	}
	return out
}

func (g *generator) buildOperation(
	path, method string,
	op *openapi.Operation,
) (*operation, error) {
	name := methodName(op.OperationID)
	if name == "" {
		name = methodName(deriveOperationID(method, path))
	}
	out := &operation{
		Name:        g.methodNames.claim(name),
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Deprecated:  op.Deprecated,
	}

	params, err := g.buildParams(out.Name, op.Parameters)
	if err != nil {
		return nil, err
	}
	out.Params = params

	body, err := g.buildBody(out.Name, op.RequestBody)
	if err != nil {
		return nil, err
	}
	out.Body = body

	if err := g.buildResponses(out, op.Responses); err != nil {
		return nil, err
	}

	out.PathParts, err = buildPathParts(path, params)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 || body != nil {
		out.ParamsType = g.typeNames.claim(out.Name + "Params")
	}
	return out, nil
}

func (g *generator) buildParams(
	opName string,
	params []*openapi.Parameter,
) ([]opParam, error) {
	ordered := slices.Clone(params)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].In != ordered[j].In {
			return paramOrder(ordered[i].In) < paramOrder(ordered[j].In)
		}
		return ordered[i].Name < ordered[j].Name
	})
	fields := newNameSet(nil)
	out := make([]opParam, 0, len(ordered))
	for _, p := range ordered {
		if p.In != "path" && p.In != "query" && p.In != "header" {
			continue
		}
		required := p.Required || p.In == "path"
		base, err := g.reg.baseType(p.Schema, opName+exportedName(p.Name))
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", p.Name, err)
		}
		typ := base
		if !required && !isNilable(base) {
			typ = "*" + base
		}
		out = append(out, opParam{
			Field:      fields.claim(exportedName(p.Name)),
			Wire:       p.Name,
			In:         p.In,
			Type:       typ,
			Base:       base,
			Required:   required,
			IsSlice:    strings.HasPrefix(base, "[]") && base != "[]byte",
			Doc:        p.Description,
			Deprecated: p.Deprecated,
		})
	}
	return out, nil
}

func paramOrder(in string) int {
	switch in {
	case "path":
		return 0
	case "query":
		return 1
	case "header":
		return 2
	}
	return 3
}

// buildPathParts splits a route path template into literals and parameter
// substitutions, resolving each "{name}" to the params-struct field that
// supplies it.
func buildPathParts(path string, params []opParam) ([]pathPart, error) {
	byWire := map[string]opParam{}
	for _, p := range params {
		if p.In == "path" {
			byWire[p.Wire] = p
		}
	}
	var (
		parts []pathPart
		lit   strings.Builder
	)
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, pathPart{Literal: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			lit.WriteByte(path[i])
			continue
		}
		end := strings.IndexByte(path[i:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unterminated '{' in path %q", path)
		}
		name := path[i+1 : i+end]
		i += end
		if name == "$" {
			continue
		}
		wildcard := strings.HasSuffix(name, "...")
		name = strings.TrimSuffix(name, "...")
		if name == "" {
			continue
		}
		p, ok := byWire[name]
		if !ok {
			return nil, fmt.Errorf(
				"path %q references {%s} with no matching parameter",
				path, name,
			)
		}
		flush()
		parts = append(parts, pathPart{Field: p.Field, Wildcard: wildcard})
	}
	flush()
	return parts, nil
}

// contentTypePreference ranks request body media types so that an
// operation advertising several encodings always generates against the
// same one.
func contentTypePreference(ct string) int {
	switch {
	case ct == "application/json":
		return 0
	case ct == "multipart/form-data":
		return 1
	case ct == "application/x-www-form-urlencoded":
		return 2
	case isSequentialJSON(ct):
		return 3
	case ct == "text/event-stream":
		return 4
	case strings.HasPrefix(ct, "multipart/"):
		return 5
	}
	return 6
}

func isSequentialJSON(ct string) bool {
	switch ct {
	case "application/jsonl",
		"application/x-ndjson",
		"application/json-seq",
		"application/geo+json-seq":
		return true
	}
	return false
}

func isRecordSeparated(ct string) bool {
	return ct == "application/json-seq" || ct == "application/geo+json-seq"
}

// pickContent selects the single media type to generate against.
func pickContent(content map[string]*openapi.MediaType) string {
	if len(content) == 0 {
		return ""
	}
	keys := slices.Sorted(maps.Keys(content))
	best := keys[0]
	for _, k := range keys[1:] {
		if contentTypePreference(k) < contentTypePreference(best) {
			best = k
		}
	}
	return best
}

func (g *generator) buildBody(
	opName string,
	rb *openapi.RequestBody,
) (*bodySpec, error) {
	if rb == nil || len(rb.Content) == 0 {
		return nil, nil
	}
	ct := pickContent(rb.Content)
	mt := rb.Content[ct]
	spec := &bodySpec{ContentType: ct}

	switch {
	case ct == "multipart/form-data", ct == "application/x-www-form-urlencoded":
		spec.Kind = bodyURLEncoded
		if ct == "multipart/form-data" {
			spec.Kind = bodyMultipart
		}
		if err := g.buildFormFields(spec, opName, mt); err != nil {
			return nil, err
		}
		return spec, nil
	case isSequentialJSON(ct):
		spec.Kind = bodyJSONStream
		spec.FramerRS = isRecordSeparated(ct)
		item, err := g.reg.baseType(mt.ItemSchema, opName+"RequestItem")
		if err != nil {
			return nil, fmt.Errorf("request body item: %w", err)
		}
		spec.ItemType = item
		return spec, nil
	case mt == nil || mt.Schema == nil:
		spec.Kind = bodyBinary
		return spec, nil
	case isBinarySchema(mt.Schema):
		spec.Kind = bodyBinary
		return spec, nil
	}

	typ, err := g.reg.baseType(mt.Schema, opName+"Request")
	if err != nil {
		return nil, fmt.Errorf("request body: %w", err)
	}
	spec.Kind = bodyJSON
	spec.Type = typ
	return spec, nil
}

func isBinarySchema(s *openapi.Schema) bool {
	return s != nil && s.Type == "string" && s.Format == "binary"
}

func (g *generator) buildFormFields(
	spec *bodySpec,
	opName string,
	mt *openapi.MediaType,
) error {
	if mt == nil || mt.Schema == nil {
		return nil
	}
	required := make(map[string]bool, len(mt.Schema.Required))
	for _, name := range mt.Schema.Required {
		required[name] = true
	}
	fields := newNameSet(nil)
	for _, wire := range slices.Sorted(maps.Keys(mt.Schema.Properties)) {
		prop := mt.Schema.Properties[wire]
		goName := fields.claim(exportedName(wire))
		if isBinarySchema(prop) || isBinaryArraySchema(prop) {
			spec.Files = append(spec.Files, formField{
				Field:    goName,
				Wire:     wire,
				Type:     fileFieldType(prop),
				Required: required[wire],
				IsSlice:  isBinaryArraySchema(prop),
			})
			continue
		}
		base, err := g.reg.baseType(prop, opName+goName)
		if err != nil {
			return fmt.Errorf("form field %q: %w", wire, err)
		}
		typ := base
		if !required[wire] && !isNilable(base) {
			typ = "*" + base
		}
		spec.Fields = append(spec.Fields, formField{
			Field:    goName,
			Wire:     wire,
			Type:     typ,
			Base:     base,
			Required: required[wire],
			IsSlice:  strings.HasPrefix(base, "[]") && base != "[]byte",
		})
	}
	return nil
}

func isBinaryArraySchema(s *openapi.Schema) bool {
	return s != nil && s.Type == "array" && isBinarySchema(s.Items)
}

func fileFieldType(s *openapi.Schema) string {
	if isBinaryArraySchema(s) {
		return "[]FormFile"
	}
	return "*FormFile"
}

// buildResponses picks the operation's success response and records every
// other declared status as a typed error case.
//
// The success response is the lowest declared 2xx. Any further 2xx that
// declares a different body type cannot be expressed in a single typed
// return, so it is recorded in Extra2xx and the generated method notes it
// rather than silently pretending the body was the success type.
func (g *generator) buildResponses(
	out *operation,
	responses map[string]*openapi.Response,
) error {
	var codes []int
	for key := range responses {
		if key == "default" {
			continue
		}
		code, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		codes = append(codes, code)
	}
	sort.Ints(codes)

	successStatus := 0
	for _, code := range codes {
		if code >= 200 && code < 300 {
			successStatus = code
			break
		}
	}

	if successStatus != 0 {
		resp := responses[strconv.Itoa(successStatus)]
		if err := g.applySuccess(out, successStatus, resp); err != nil {
			return err
		}
	}

	for _, code := range codes {
		if code == successStatus {
			continue
		}
		if code >= 200 && code < 300 {
			out.Extra2xx = append(out.Extra2xx, code)
			continue
		}
		typ, err := g.responseBodyType(
			out.Name,
			code,
			responses[strconv.Itoa(code)],
		)
		if err != nil {
			return err
		}
		out.Errors = append(out.Errors, errorSpec{Status: code, Type: typ})
	}

	if def, ok := responses["default"]; ok {
		typ, err := g.responseBodyType(out.Name, 0, def)
		if err != nil {
			return err
		}
		out.Default = &errorSpec{Type: typ}
	}
	return nil
}

func (g *generator) applySuccess(
	out *operation,
	status int,
	resp *openapi.Response,
) error {
	out.Success = responseSpec{Status: status}
	if resp == nil || len(resp.Content) == 0 {
		return nil
	}
	ct := pickContent(resp.Content)
	mt := resp.Content[ct]
	out.Accept = ct

	switch {
	case ct == "text/event-stream":
		out.Stream = &streamSpec{Kind: streamSSE, Status: status}
		return nil
	case strings.HasPrefix(ct, "multipart/"):
		out.Stream = &streamSpec{Kind: streamMultipart, Status: status}
		return nil
	case isSequentialJSON(ct):
		item, err := g.reg.baseType(itemSchemaOf(mt), out.Name+"Item")
		if err != nil {
			return fmt.Errorf("response item schema: %w", err)
		}
		out.Stream = &streamSpec{
			Kind:     streamJSON,
			Status:   status,
			ItemType: item,
		}
		return nil
	}

	if mt == nil || mt.Schema == nil {
		return nil
	}
	typ, err := g.reg.baseType(mt.Schema, out.Name+"Response")
	if err != nil {
		return fmt.Errorf("response body: %w", err)
	}
	out.Success.Type = typ
	return nil
}

func itemSchemaOf(mt *openapi.MediaType) *openapi.Schema {
	if mt == nil {
		return nil
	}
	if mt.ItemSchema != nil {
		return mt.ItemSchema
	}
	return mt.Schema
}

// responseBodyType resolves the Go type an error response decodes into,
// or "" when the response declares no JSON body.
func (g *generator) responseBodyType(
	opName string,
	status int,
	resp *openapi.Response,
) (string, error) {
	if resp == nil || len(resp.Content) == 0 {
		return "", nil
	}
	mt, ok := resp.Content["application/json"]
	if !ok {
		ct := pickContent(resp.Content)
		if !strings.HasSuffix(ct, "+json") {
			return "", nil
		}
		mt = resp.Content[ct]
	}
	if mt == nil || mt.Schema == nil {
		return "", nil
	}
	hint := opName + "Error"
	if status > 0 {
		hint = opName + strconv.Itoa(status) + "Error"
	}
	typ, err := g.reg.baseType(mt.Schema, hint)
	if err != nil {
		return "", fmt.Errorf("error response %d: %w", status, err)
	}
	return typ, nil
}

// deriveOperationID mirrors the fallback the openapi package uses when an
// operation carries no explicit operationId, so a spec produced by another
// tool without operationIds still yields usable method names.
func deriveOperationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, raw := range strings.Split(path, "/") {
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
			name := strings.TrimSuffix(raw[1:len(raw)-1], "...")
			if name == "" || name == "$" {
				continue
			}
			b.WriteString("By")
			b.WriteString(exportedName(name))
			continue
		}
		b.WriteString(exportedName(raw))
	}
	return b.String()
}
