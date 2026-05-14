package router

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// paramsBinder reads HTTP request data and populates a Params struct.
type paramsBinder func(*http.Request) (reflect.Value, error)

type fieldSource int

const (
	srcPath fieldSource = iota
	srcQuery
	srcHeader
	srcForm
	srcFile
	srcBody
)

func (s fieldSource) String() string {
	switch s {
	case srcPath:
		return "path"
	case srcQuery:
		return "query"
	case srcHeader:
		return "header"
	case srcForm:
		return "form"
	case srcFile:
		return "file"
	case srcBody:
		return "body"
	}
	return "?"
}

type fieldBinder struct {
	index    []int
	source   fieldSource
	key      string
	required bool     // form/file: non-pointer/non-slice
	allowed  []string // contentType allow-list (file/body stream)
	kind     bodyKind // only used when source == srcBody
}

// bodyKind classifies the runtime shape of a body:"" field.
type bodyKind int

const (
	bodyJSON bodyKind = iota
	bodyReader
	bodyBytes
)

var (
	fileHeaderPtrType = reflect.TypeFor[*multipart.FileHeader]()
	fileHeaderSlcType = reflect.TypeFor[[]*multipart.FileHeader]()
	readerType        = reflect.TypeFor[io.Reader]()
	bytesType         = reflect.TypeFor[[]byte]()
)

// buildBinder reflects on a Params struct type once at registration and
// returns a closure that populates a fresh instance from a request.
func buildBinder(t reflect.Type, cfg bindConfig) (paramsBinder, error) {
	binders, err := collectBinders(t, nil)
	if err != nil {
		return nil, err
	}
	if err := validateExclusivity(binders); err != nil {
		return nil, err
	}

	needForm := false
	for _, fb := range binders {
		if fb.source == srcForm || fb.source == srcFile {
			needForm = true
			break
		}
	}

	return func(r *http.Request) (reflect.Value, error) {
		v := reflect.New(t).Elem()
		if needForm {
			if err := parseRequestForm(r, cfg.maxMultipartMemory); err != nil {
				return reflect.Value{}, BadRequest("form: " + err.Error())
			}
		}
		for _, fb := range binders {
			field := v.FieldByIndex(fb.index)
			if err := applyBinder(field, fb, r, cfg); err != nil {
				return reflect.Value{}, err
			}
		}
		return v, nil
	}, nil
}

func applyBinder(
	field reflect.Value,
	fb fieldBinder,
	r *http.Request,
	cfg bindConfig,
) error {
	switch fb.source {
	case srcPath:
		if err := setScalar(field, r.PathValue(fb.key)); err != nil {
			return BadRequest(fmt.Sprintf(
				"path parameter %q: %v", fb.key, err,
			))
		}
	case srcQuery:
		raw := r.URL.Query().Get(fb.key)
		if raw == "" {
			return nil
		}
		if err := setScalar(field, raw); err != nil {
			return BadRequest(fmt.Sprintf(
				"query parameter %q: %v", fb.key, err,
			))
		}
	case srcHeader:
		raw := r.Header.Get(fb.key)
		if raw == "" {
			return nil
		}
		if err := setScalar(field, raw); err != nil {
			return BadRequest(fmt.Sprintf(
				"header %q: %v", fb.key, err,
			))
		}
	case srcForm:
		return bindFormField(field, fb, r)
	case srcFile:
		return bindFileField(field, fb, r)
	case srcBody:
		return bindBodyField(field, fb, r, cfg)
	}
	return nil
}

// parseRequestForm parses both multipart/form-data and
// application/x-www-form-urlencoded request bodies. It is idempotent: the
// underlying net/http parsers populate r.Form / r.PostForm / r.MultipartForm
// once and remember the result, so calling this multiple times on the same
// request is safe and cheap.
func parseRequestForm(r *http.Request, maxMem int64) error {
	ct := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if mediaType == "multipart/form-data" {
		if maxMem <= 0 {
			maxMem = defaultMaxMultipartMemory
		}
		return r.ParseMultipartForm(maxMem)
	}
	return r.ParseForm()
}

func bindFormField(
	field reflect.Value,
	fb fieldBinder,
	r *http.Request,
) error {
	values, present := r.PostForm[fb.key]
	if !present || len(values) == 0 {
		if fb.required {
			return BadRequest(fmt.Sprintf(
				"form field %q: required", fb.key,
			))
		}
		return nil
	}
	if field.Kind() == reflect.Slice {
		out := reflect.MakeSlice(field.Type(), len(values), len(values))
		for i, raw := range values {
			if err := setScalar(out.Index(i), raw); err != nil {
				return BadRequest(fmt.Sprintf(
					"form field %q[%d]: %v", fb.key, i, err,
				))
			}
		}
		field.Set(out)
		return nil
	}
	if err := setScalar(field, values[0]); err != nil {
		return BadRequest(fmt.Sprintf(
			"form field %q: %v", fb.key, err,
		))
	}
	return nil
}

func bindFileField(
	field reflect.Value,
	fb fieldBinder,
	r *http.Request,
) error {
	if r.MultipartForm == nil {
		if fb.required {
			return BadRequest(fmt.Sprintf(
				"file field %q: requires multipart/form-data", fb.key,
			))
		}
		return nil
	}
	headers := r.MultipartForm.File[fb.key]
	if len(headers) == 0 {
		if fb.required {
			return BadRequest(fmt.Sprintf(
				"file field %q: required", fb.key,
			))
		}
		return nil
	}
	for _, h := range headers {
		if err := checkContentType(h, fb.allowed); err != nil {
			return BadRequest(fmt.Sprintf(
				"file field %q: %v", fb.key, err,
			))
		}
	}
	if field.Type() == fileHeaderSlcType {
		field.Set(reflect.ValueOf(headers))
		return nil
	}
	field.Set(reflect.ValueOf(headers[0]))
	return nil
}

func bindBodyField(
	field reflect.Value,
	fb fieldBinder,
	r *http.Request,
	cfg bindConfig,
) error {
	switch fb.kind {
	case bodyReader:
		if len(fb.allowed) > 0 {
			if err := checkBodyContentType(r, fb.allowed); err != nil {
				return BadRequest("body: " + err.Error())
			}
		}
		field.Set(reflect.ValueOf(r.Body))
		return nil
	case bodyBytes:
		if len(fb.allowed) > 0 {
			if err := checkBodyContentType(r, fb.allowed); err != nil {
				return BadRequest("body: " + err.Error())
			}
		}
		limit := cfg.maxMultipartMemory
		if limit <= 0 {
			limit = defaultMaxMultipartMemory
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err != nil {
			return BadRequest("body: " + err.Error())
		}
		if int64(len(data)) > limit {
			return BadRequest(fmt.Sprintf(
				"body: exceeds max size of %d bytes", limit,
			))
		}
		field.SetBytes(data)
		return nil
	default:
		if err := cfg.codec.Decode(r.Body, field.Addr().Interface()); err != nil {
			return BadRequest("body: " + err.Error())
		}
		return nil
	}
}

func checkContentType(h *multipart.FileHeader, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	got := h.Header.Get("Content-Type")
	if matchMediaType(got, allowed) {
		return nil
	}
	return fmt.Errorf(
		"content type %q not allowed (want %s)",
		got,
		strings.Join(allowed, ", "),
	)
}

func checkBodyContentType(r *http.Request, allowed []string) error {
	got := r.Header.Get("Content-Type")
	if matchMediaType(got, allowed) {
		return nil
	}
	return fmt.Errorf(
		"content type %q not allowed (want %s)",
		got,
		strings.Join(allowed, ", "),
	)
}

func matchMediaType(got string, allowed []string) bool {
	gotType, _, _ := mime.ParseMediaType(got)
	if gotType == "" {
		gotType = got
	}
	for _, a := range allowed {
		if a == gotType {
			return true
		}
		// wildcard like image/*
		if strings.HasSuffix(a, "/*") {
			prefix := strings.TrimSuffix(a, "/*")
			if strings.HasPrefix(gotType, prefix+"/") {
				return true
			}
		}
	}
	return false
}

// collectBinders walks the Params struct (flat, only top-level fields) and
// produces a fieldBinder per exported tagged field. Tag precedence is fixed:
// path > query > header > form > file > body. Tag validity is checked here
// (e.g. file: must target *multipart.FileHeader, form: must target a scalar
// or scalar slice).
func collectBinders(t reflect.Type, parent []int) ([]fieldBinder, error) {
	var out []fieldBinder
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		idx := append(append([]int(nil), parent...), i)
		fb, ok, err := classifyField(f, idx)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, fb)
		}
	}
	return out, nil
}

func classifyField(
	f reflect.StructField,
	idx []int,
) (fieldBinder, bool, error) {
	if v, ok := f.Tag.Lookup("path"); ok {
		return fieldBinder{index: idx, source: srcPath, key: v}, true, nil
	}
	if v, ok := f.Tag.Lookup("query"); ok {
		return fieldBinder{index: idx, source: srcQuery, key: v}, true, nil
	}
	if v, ok := f.Tag.Lookup("header"); ok {
		return fieldBinder{index: idx, source: srcHeader, key: v}, true, nil
	}
	if v, ok := f.Tag.Lookup("form"); ok {
		if err := validateFormFieldType(f); err != nil {
			return fieldBinder{}, false, err
		}
		return fieldBinder{
			index:    idx,
			source:   srcForm,
			key:      v,
			required: isFormRequired(f.Type),
		}, true, nil
	}
	if v, ok := f.Tag.Lookup("file"); ok {
		if err := validateFileFieldType(f); err != nil {
			return fieldBinder{}, false, err
		}
		return fieldBinder{
			index:    idx,
			source:   srcFile,
			key:      v,
			required: f.Type == fileHeaderPtrType,
			allowed:  splitContentTypes(f.Tag.Get("contentType")),
		}, true, nil
	}
	if _, ok := f.Tag.Lookup("body"); ok {
		kind, err := classifyBodyKind(f.Type)
		if err != nil {
			return fieldBinder{}, false, err
		}
		return fieldBinder{
			index:   idx,
			source:  srcBody,
			kind:    kind,
			allowed: splitContentTypes(f.Tag.Get("contentType")),
		}, true, nil
	}
	return fieldBinder{}, false, nil
}

// validateExclusivity rejects Params structs that mix a body:"" field with
// form: or file: fields; those are different request shapes and combining
// them would generate an ambiguous OpenAPI spec.
func validateExclusivity(fbs []fieldBinder) error {
	var hasForm, hasFile, hasBody bool
	for _, fb := range fbs {
		switch fb.source {
		case srcForm:
			hasForm = true
		case srcFile:
			hasFile = true
		case srcBody:
			hasBody = true
		}
	}
	if hasBody && (hasForm || hasFile) {
		return fmt.Errorf(
			"params struct mixes body with form/file; pick one request shape",
		)
	}
	return nil
}

func validateFormFieldType(f reflect.StructField) error {
	t := f.Type
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		t = t.Elem()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
	}
	if !isScalarKind(t.Kind()) {
		return fmt.Errorf(
			"form field %q: unsupported type %s (want scalar or []scalar)",
			f.Name, f.Type,
		)
	}
	return nil
}

func validateFileFieldType(f reflect.StructField) error {
	if f.Type == fileHeaderPtrType || f.Type == fileHeaderSlcType {
		return nil
	}
	return fmt.Errorf(
		"file field %q: want *router.FormFile or []*router.FormFile, got %s",
		f.Name, f.Type,
	)
}

func classifyBodyKind(t reflect.Type) (bodyKind, error) {
	if t == readerType {
		return bodyReader, nil
	}
	if t == bytesType {
		return bodyBytes, nil
	}
	// Anything else falls through to the codec (JSON by default).
	return bodyJSON, nil
}

func isFormRequired(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice:
		return false
	}
	return true
}

func isScalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.String,
		reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
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

func setScalar(v reflect.Value, raw string) error {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return setScalar(v.Elem(), raw)
	}
	if raw == "" && v.Kind() != reflect.String {
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := v.Type().Bits()
		n, err := strconv.ParseInt(raw, 10, bits)
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		bits := v.Type().Bits()
		n, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		bits := v.Type().Bits()
		f, err := strconv.ParseFloat(raw, bits)
		if err != nil {
			return err
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported field type %s", v.Type())
	}
	return nil
}
