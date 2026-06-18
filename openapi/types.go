package openapi

// Document is the root OpenAPI document. Field order in JSON output follows
// the conventional OpenAPI 3.2 layout: openapi, info, dialect, servers,
// paths, components, security, tags, externalDocs.
type Document struct {
	OpenAPI           string                `json:"openapi"`
	Info              Info                  `json:"info"`
	JSONSchemaDialect string                `json:"jsonSchemaDialect,omitempty"`
	Servers           []*Server             `json:"servers,omitempty"`
	Paths             map[string]*PathItem  `json:"paths"`
	Components        *Components           `json:"components,omitempty"`
	Security          []SecurityRequirement `json:"security,omitempty"`
	Tags              []*Tag                `json:"tags,omitempty"`
	ExternalDocs      *ExternalDocs         `json:"externalDocs,omitempty"`
}

// Tag is a Tag Object — operation-grouping metadata referenced by name
// from individual operations. OAS 3.2 promotes the Tag Object to a
// first-class navigational element: Summary is a short human label,
// Parent references another tag to build nested groups (one of 3.2's
// headline features), and Kind is a free-form classifier (e.g. "nav",
// "audience") that lets renderers decide how to surface the tag.
type Tag struct {
	Name         string        `json:"name"`
	Summary      string        `json:"summary,omitempty"`
	Description  string        `json:"description,omitempty"`
	Parent       string        `json:"parent,omitempty"`
	Kind         string        `json:"kind,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty"`
}

// ExternalDocs links out to additional human-readable documentation
// for a tag, operation, or document as a whole.
type ExternalDocs struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Server is one entry in the document-level servers array (OAS 3.2 §4.7).
// URL may contain {variable} placeholders; each must have a matching entry
// in Variables. Set Variables to nil for fully literal URLs.
type Server struct {
	URL         string                     `json:"url"`
	Description string                     `json:"description,omitempty"`
	Variables   map[string]*ServerVariable `json:"variables,omitempty"`
}

// ServerVariable describes a single {variable} placeholder in a Server URL.
// Default is required; Enum constrains the allowed values when non-empty.
type ServerVariable struct {
	Default     string   `json:"default"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
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
//
// Security is a pointer-to-slice so we can distinguish three states: nil
// means "inherit the document-level default", a non-nil empty slice means
// "explicitly clear security for this operation" (emits "security": []),
// and a non-empty slice lists the alternative requirements that satisfy
// the operation.
type Operation struct {
	Tags         []string               `json:"tags,omitempty"`
	Summary      string                 `json:"summary,omitempty"`
	Description  string                 `json:"description,omitempty"`
	ExternalDocs *ExternalDocs          `json:"externalDocs,omitempty"`
	OperationID  string                 `json:"operationId,omitempty"`
	Deprecated   bool                   `json:"deprecated,omitempty"`
	Parameters   []*Parameter           `json:"parameters,omitempty"`
	RequestBody  *RequestBody           `json:"requestBody,omitempty"`
	Responses    map[string]*Response   `json:"responses"`
	Security     *[]SecurityRequirement `json:"security,omitempty"`
}

// Parameter is a path, query, or header parameter.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Deprecated  bool    `json:"deprecated,omitempty"`
	Schema      *Schema `json:"schema"`
}

// RequestBody is the typed body of an operation.
type RequestBody struct {
	Required bool                  `json:"required,omitempty"`
	Content  map[string]*MediaType `json:"content"`
}

// Response is a single status-code response variant.
type Response struct {
	Description string                `json:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
}

// MediaType is the body shape for a single content type (e.g. application/json).
//
// OAS 3.2 adds itemSchema (applied per-item in sequential media types such as
// application/jsonl, application/json-seq, text/event-stream, multipart/mixed),
// and itemEncoding / prefixEncoding for positional multipart streams.
type MediaType struct {
	Schema         *Schema              `json:"schema,omitempty"`
	ItemSchema     *Schema              `json:"itemSchema,omitempty"`
	Encoding       map[string]*Encoding `json:"encoding,omitempty"`
	PrefixEncoding []*Encoding          `json:"prefixEncoding,omitempty"`
	ItemEncoding   *Encoding            `json:"itemEncoding,omitempty"`
}

// Encoding is the OAS 3.2 Encoding Object used by form, multipart, and
// multipart/mixed media types to attach per-property or per-position
// metadata such as an allowed Content-Type list, custom Headers, and
// (for nested multipart) further positional encoding rules.
type Encoding struct {
	ContentType    string             `json:"contentType,omitempty"`
	Headers        map[string]*Header `json:"headers,omitempty"`
	PrefixEncoding []*Encoding        `json:"prefixEncoding,omitempty"`
	ItemEncoding   *Encoding          `json:"itemEncoding,omitempty"`
}

// Header describes a single header — either a fixed header on a multipart
// part (used inside Encoding.Headers) or a response header advertised on
// a Response Object (used inside Response.Headers).
//
// Description is human-readable text rendered by tools like Scalar above
// the header name. Schema describes the value; when nil the renderer
// assumes a plain string.
type Header struct {
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// Components is the document's reusable-definitions block.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityRequirement is a map from security scheme name (or URI) to the
// list of scopes (oauth2/openIdConnect) or role names required to satisfy
// it. Multiple keys within a single requirement are AND-combined; multiple
// SecurityRequirement entries in an array are OR-combined.
type SecurityRequirement map[string][]string

// SecurityScheme defines a single auth mechanism that operations may
// reference by name. Per OAS 3.2 §4.27, Type is one of "apiKey", "http",
// "mutualTLS", "oauth2", "openIdConnect"; the field set required varies
// by Type and is enforced by the spec rather than this struct.
//
// OAS 3.2 additions: oauth2MetadataUrl (oauth2) and the deviceAuthorization
// OAuth flow.
type SecurityScheme struct {
	Type              string      `json:"type"`
	Description       string      `json:"description,omitempty"`
	Name              string      `json:"name,omitempty"`
	In                string      `json:"in,omitempty"`
	Scheme            string      `json:"scheme,omitempty"`
	BearerFormat      string      `json:"bearerFormat,omitempty"`
	Flows             *OAuthFlows `json:"flows,omitempty"`
	OpenIDConnectURL  string      `json:"openIdConnectUrl,omitempty"`
	OAuth2MetadataURL string      `json:"oauth2MetadataUrl,omitempty"`
	Deprecated        bool        `json:"deprecated,omitempty"`
}

// OAuthFlows enumerates the OAuth2 flows a security scheme supports. OAS
// 3.2 introduces DeviceAuthorization (RFC 8628) alongside the four classic
// flows.
type OAuthFlows struct {
	Implicit            *OAuthFlow `json:"implicit,omitempty"`
	Password            *OAuthFlow `json:"password,omitempty"`
	ClientCredentials   *OAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode   *OAuthFlow `json:"authorizationCode,omitempty"`
	DeviceAuthorization *OAuthFlow `json:"deviceAuthorization,omitempty"`
}

// OAuthFlow is a single OAuth flow's configuration. The URL fields apply
// only to certain flows per OAS 3.2 §4.29; Scopes is required and emitted
// even when empty.
type OAuthFlow struct {
	AuthorizationURL       string            `json:"authorizationUrl,omitempty"`
	DeviceAuthorizationURL string            `json:"deviceAuthorizationUrl,omitempty"`
	TokenURL               string            `json:"tokenUrl,omitempty"`
	RefreshURL             string            `json:"refreshUrl,omitempty"`
	Scopes                 map[string]string `json:"scopes"`
}

// Schema is a JSON Schema 2020-12 fragment. Only the fields minmux emits are
// defined here; extend as needed.
//
// OAS 3.2 streaming use cases pull in oneOf/const for SSE event-name dispatch
// and contentMediaType/contentSchema for validating JSON-in-string fields
// (e.g. the SSE data field).
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Format               string             `json:"format,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	MinLength            *int               `json:"minLength,omitempty"`
	MaxLength            *int               `json:"maxLength,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Default              any                `json:"default,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	Const                any                `json:"const,omitempty"`
	ContentMediaType     string             `json:"contentMediaType,omitempty"`
	ContentSchema        *Schema            `json:"contentSchema,omitempty"`
}
