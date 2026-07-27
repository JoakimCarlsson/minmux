package clientgen

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/joakimcarlsson/minmux/openapi"
)

const componentRefPrefix = "#/components/schemas/"

// typeKind classifies what a namedType renders as.
type typeKind int

const (
	kindStruct typeKind = iota
	kindUnion
	kindAlias
)

// namedType is one Go type declaration in the generated types file.
type namedType struct {
	Name   string
	Kind   typeKind
	Doc    string
	Fields []structField
	Union  *unionInfo
	Alias  string
}

// structField is one field of a generated struct.
type structField struct {
	Name      string
	Type      string
	JSONName  string
	OmitEmpty bool
	Doc       string
}

// unionInfo describes a discriminated union: the property carrying the
// variant tag, and the variants it selects between.
type unionInfo struct {
	Discriminator string
	Variants      []unionVariant
}

type unionVariant struct {
	Value string
	Type  string
}

// registry resolves OpenAPI schemas to Go type expressions, hoisting every
// component schema into a named declaration and synthesizing names for the
// anonymous object and union schemas that appear inline.
type registry struct {
	names     *nameSet
	byRef     map[string]string
	types     map[string]*namedType
	synthetic map[string]string
	building  map[string]bool
}

func newRegistry() *registry {
	return &registry{
		names:     newNameSet(reservedNames),
		byRef:     map[string]string{},
		types:     map[string]*namedType{},
		synthetic: map[string]string{},
		building:  map[string]bool{},
	}
}

// declare pre-allocates a Go name for every component schema before any
// body is resolved, so that a $ref encountered while building one type
// already knows the final name of its target even when the two refer to
// each other.
func (r *registry) declare(doc *openapi.Document) {
	if doc.Components == nil {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(doc.Components.Schemas)) {
		r.byRef[componentRefPrefix+key] = r.names.claim(typeName(key))
	}
}

// build resolves every declared component schema into a namedType.
func (r *registry) build(doc *openapi.Document) error {
	if doc.Components == nil {
		return nil
	}
	for _, key := range slices.Sorted(maps.Keys(doc.Components.Schemas)) {
		name := r.byRef[componentRefPrefix+key]
		if _, done := r.types[name]; done {
			continue
		}
		if err := r.buildNamed(name, doc.Components.Schemas[key]); err != nil {
			return fmt.Errorf("component schema %q: %w", key, err)
		}
	}
	return nil
}

func (r *registry) buildNamed(name string, s *openapi.Schema) error {
	if r.building[name] {
		return nil
	}
	r.building[name] = true
	defer delete(r.building, name)

	if s == nil {
		r.types[name] = &namedType{Name: name, Kind: kindAlias, Alias: "any"}
		return nil
	}
	if isUnion(s) {
		u, err := r.unionInfoFor(s)
		if err != nil {
			return err
		}
		r.types[name] = &namedType{Name: name, Kind: kindUnion, Union: u}
		return nil
	}
	if s.Type == "object" && len(s.Properties) > 0 {
		fields, err := r.structFields(s, name)
		if err != nil {
			return err
		}
		r.types[name] = &namedType{
			Name:   name,
			Kind:   kindStruct,
			Doc:    s.Description,
			Fields: fields,
		}
		return nil
	}
	expr, err := r.goType(s, true, name)
	if err != nil {
		return err
	}
	r.types[name] = &namedType{
		Name:  name,
		Kind:  kindAlias,
		Doc:   s.Description,
		Alias: expr,
	}
	return nil
}

// structFields converts a schema's properties into Go fields, sorted by
// wire name so output does not depend on Go's map iteration order.
func (r *registry) structFields(
	s *openapi.Schema,
	owner string,
) ([]structField, error) {
	required := make(map[string]bool, len(s.Required))
	for _, name := range s.Required {
		required[name] = true
	}
	fieldNames := newNameSet(nil)
	out := make([]structField, 0, len(s.Properties))
	for _, wire := range slices.Sorted(maps.Keys(s.Properties)) {
		prop := s.Properties[wire]
		goName := fieldNames.claim(exportedName(wire))
		expr, err := r.goType(prop, required[wire], owner+goName)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", wire, err)
		}
		doc := ""
		if prop != nil {
			doc = prop.Description
		}
		out = append(out, structField{
			Name:      goName,
			Type:      expr,
			JSONName:  wire,
			OmitEmpty: !required[wire],
			Doc:       doc,
		})
	}
	return out, nil
}

// goType returns the Go type expression for a schema.
//
// required drives the pointer decision: a property the server always sends
// maps to a value type, while an optional one becomes a pointer so the
// caller can tell "absent" from "zero". Types that are already nil-able —
// slices, maps, byte strings — are left alone rather than double-wrapped.
//
// hint seeds the name of any type that has to be synthesized because the
// schema is anonymous.
func (r *registry) goType(
	s *openapi.Schema,
	required bool,
	hint string,
) (string, error) {
	base, err := r.baseType(s, hint)
	if err != nil {
		return "", err
	}
	if required || isNilable(base) {
		return base, nil
	}
	return "*" + base, nil
}

func isNilable(expr string) bool {
	return strings.HasPrefix(expr, "[]") ||
		strings.HasPrefix(expr, "map[") ||
		expr == "any" ||
		expr == "json.RawMessage"
}

func (r *registry) baseType(s *openapi.Schema, hint string) (string, error) {
	if s == nil {
		return "json.RawMessage", nil
	}
	if s.Ref != "" {
		name, ok := r.byRef[s.Ref]
		if !ok {
			return "", fmt.Errorf("unresolved $ref %q", s.Ref)
		}
		return name, nil
	}
	if isUnion(s) {
		return r.synthesizeUnion(s, hint)
	}
	switch s.Type {
	case "string":
		return stringType(s), nil
	case "boolean":
		return "bool", nil
	case "integer":
		return integerType(s), nil
	case "number":
		if s.Format == "float" {
			return "float32", nil
		}
		return "float64", nil
	case "array":
		elem, err := r.baseType(s.Items, hint+"Item")
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case "object", "":
		return r.objectType(s, hint)
	}
	return "json.RawMessage", nil
}

func stringType(s *openapi.Schema) string {
	switch s.Format {
	case "date-time":
		return "time.Time"
	case "binary":
		return "[]byte"
	}
	return "string"
}

func integerType(s *openapi.Schema) string {
	switch s.Format {
	case "int32":
		return "int32"
	case "int64":
		return "int64"
	}
	return "int"
}

func (r *registry) objectType(
	s *openapi.Schema,
	hint string,
) (string, error) {
	if s.AdditionalProperties != nil {
		elem, err := r.baseType(s.AdditionalProperties, hint+"Value")
		if err != nil {
			return "", err
		}
		return "map[string]" + elem, nil
	}
	if len(s.Properties) == 0 {
		if s.Type == "" {
			return "json.RawMessage", nil
		}
		return "map[string]any", nil
	}
	return r.synthesizeStruct(s, hint)
}

// synthesizeStruct hoists an anonymous object schema into a named type.
// Structurally identical schemas collapse onto one declaration so that a
// shape repeated across operations does not produce near-duplicate types.
func (r *registry) synthesizeStruct(
	s *openapi.Schema,
	hint string,
) (string, error) {
	sig := "struct:" + structSignature(s)
	if name, ok := r.synthetic[sig]; ok {
		return name, nil
	}
	name := r.names.claim(typeName(hint))
	r.synthetic[sig] = name
	fields, err := r.structFields(s, name)
	if err != nil {
		return "", err
	}
	r.types[name] = &namedType{
		Name:   name,
		Kind:   kindStruct,
		Doc:    s.Description,
		Fields: fields,
	}
	return name, nil
}

func (r *registry) synthesizeUnion(
	s *openapi.Schema,
	hint string,
) (string, error) {
	u, err := r.unionInfoFor(s)
	if err != nil {
		return "", err
	}
	sig := "union:" + u.Discriminator + ":"
	for _, v := range u.Variants {
		sig += v.Value + "=" + v.Type + ","
	}
	if name, ok := r.synthetic[sig]; ok {
		return name, nil
	}
	name := r.names.claim(typeName(hint))
	r.synthetic[sig] = name
	r.types[name] = &namedType{Name: name, Kind: kindUnion, Union: u}
	return name, nil
}

func (r *registry) unionInfoFor(s *openapi.Schema) (*unionInfo, error) {
	u := &unionInfo{Discriminator: s.Discriminator.PropertyName}
	for _, value := range slices.Sorted(maps.Keys(s.Discriminator.Mapping)) {
		ref := s.Discriminator.Mapping[value]
		name, ok := r.byRef[ref]
		if !ok {
			return nil, fmt.Errorf(
				"discriminator %q maps to unresolved $ref %q",
				value, ref,
			)
		}
		u.Variants = append(u.Variants, unionVariant{Value: value, Type: name})
	}
	if len(u.Variants) == 0 {
		return nil, fmt.Errorf("discriminated union has no variant mapping")
	}
	return u, nil
}

func isUnion(s *openapi.Schema) bool {
	return s.Discriminator != nil &&
		s.Discriminator.PropertyName != "" &&
		len(s.OneOf) > 0
}

// structSignature renders a stable structural fingerprint of an object
// schema, used to collapse duplicate anonymous shapes.
func structSignature(s *openapi.Schema) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(schemaSignature(s.Properties[name]))
		b.WriteByte(';')
	}
	b.WriteString("|req=")
	b.WriteString(strings.Join(slices.Sorted(slices.Values(s.Required)), ","))
	return b.String()
}

func schemaSignature(s *openapi.Schema) string {
	if s == nil {
		return "nil"
	}
	if s.Ref != "" {
		return s.Ref
	}
	if isUnion(s) {
		return "oneOf(" + s.Discriminator.PropertyName + ")"
	}
	switch s.Type {
	case "array":
		return "[]" + schemaSignature(s.Items)
	case "object":
		if s.AdditionalProperties != nil {
			return "map[" + schemaSignature(s.AdditionalProperties) + "]"
		}
		if len(s.Properties) > 0 {
			return "{" + structSignature(s) + "}"
		}
	}
	return s.Type + "/" + s.Format
}

// sortedTypes returns every declared type in name order.
func (r *registry) sortedTypes() []*namedType {
	out := make([]*namedType, 0, len(r.types))
	for _, name := range slices.Sorted(maps.Keys(r.types)) {
		out = append(out, r.types[name])
	}
	return out
}
