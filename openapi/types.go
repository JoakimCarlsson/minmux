package openapi

// Document is the root OpenAPI document. Field order in JSON output follows
// the conventional OpenAPI 3.2 layout: openapi, info, dialect, paths, components.
type Document struct {
	OpenAPI           string               `json:"openapi"`
	Info              Info                 `json:"info"`
	JSONSchemaDialect string               `json:"jsonSchemaDialect,omitempty"`
	Paths             map[string]*PathItem `json:"paths"`
	Components        *Components          `json:"components,omitempty"`
}

// PathItem is a single path entry, carrying one operation per HTTP method.
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Head    *Operation `json:"head,omitempty"`
	Options *Operation `json:"options,omitempty"`
	Trace   *Operation `json:"trace,omitempty"`
}

// Operation describes one endpoint.
type Operation struct {
	Tags        []string             `json:"tags,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	Parameters  []*Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody         `json:"requestBody,omitempty"`
	Responses   map[string]*Response `json:"responses"`
}

// Parameter is a path, query, or header parameter.
type Parameter struct {
	Name     string  `json:"name"`
	In       string  `json:"in"`
	Required bool    `json:"required,omitempty"`
	Schema   *Schema `json:"schema"`
}

// RequestBody is the typed body of an operation.
type RequestBody struct {
	Required bool                  `json:"required,omitempty"`
	Content  map[string]*MediaType `json:"content"`
}

// Response is a single status-code response variant.
type Response struct {
	Description string                `json:"description"`
	Content     map[string]*MediaType `json:"content,omitempty"`
}

// MediaType is the body shape for a single content type (e.g. application/json).
type MediaType struct {
	Schema   *Schema              `json:"schema,omitempty"`
	Encoding map[string]*Encoding `json:"encoding,omitempty"`
}

// Encoding is the OAS 3.2 Encoding Object used by multipart and
// x-www-form-urlencoded media types to attach per-property metadata such
// as an allowed Content-Type list.
type Encoding struct {
	ContentType string `json:"contentType,omitempty"`
}

// Components is the document's reusable-definitions block.
type Components struct {
	Schemas map[string]*Schema `json:"schemas,omitempty"`
}

// Schema is a JSON Schema 2020-12 fragment. Only the fields minmux emits are
// defined here; extend as needed.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
}
