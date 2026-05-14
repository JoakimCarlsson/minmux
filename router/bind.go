package router

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
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
	itemType reflect.Type
	iterType reflect.Type
}

// bodyKind classifies the runtime shape of a body:"" field.
type bodyKind int

const (
	bodyJSON bodyKind = iota
	bodyReader
	bodyBytes
	// bodyIterJSON groups every line- or RS-framed sequential JSON
	// media type (application/jsonl, application/x-ndjson,
	// application/json-seq, application/geo+json-seq). The actual
	// framer is picked per request from the Content-Type header so a
	// single endpoint can accept any of them with the same handler.
	bodyIterJSON
	// bodyIterSSE: iter.Seq2[SSEEvent, error], text/event-stream.
	bodyIterSSE
	// bodyIterMultipart: iter.Seq2[Part, error], multipart/mixed or
	// any multipart/* (boundary parsed from Content-Type).
	bodyIterMultipart
)

var (
	fileHeaderPtrType = reflect.TypeFor[*multipart.FileHeader]()
	fileHeaderSlcType = reflect.TypeFor[[]*multipart.FileHeader]()
	readerType        = reflect.TypeFor[io.Reader]()
	bytesType         = reflect.TypeFor[[]byte]()
	errorType         = reflect.TypeFor[error]()
	sseEventType      = reflect.TypeFor[SSEEvent]()
	partType          = reflect.TypeFor[Part]()
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
	case bodyIterJSON, bodyIterSSE, bodyIterMultipart:
		if err := checkBodyContentType(r, fb.allowed); err != nil {
			return BadRequest("body: " + err.Error())
		}
		seq, err := buildBodyIterator(r, fb, cfg)
		if err != nil {
			return err
		}
		field.Set(seq)
		return nil
	default:
		if err := cfg.codec.Decode(r.Body, field.Addr().Interface()); err != nil {
			return BadRequest("body: " + err.Error())
		}
		return nil
	}
}

// buildBodyIterator constructs the iter.Seq2[T, error] value that the
// handler will range over. The returned reflect.Value matches fb.iterType
// so it can be Set directly on the params field.
func buildBodyIterator(
	r *http.Request,
	fb fieldBinder,
	cfg bindConfig,
) (reflect.Value, error) {
	switch fb.kind {
	case bodyIterJSON:
		framer := jsonStreamFramer(r.Header.Get("Content-Type"))
		seq := jsonStreamIterator(r.Body, fb.itemType, cfg.codec, framer)
		return adaptIterSeq2(seq, fb.iterType), nil
	case bodyIterSSE:
		seq := sseIterator(r.Body)
		return reflect.ValueOf(seq).Convert(fb.iterType), nil
	case bodyIterMultipart:
		boundary, err := multipartBoundary(r)
		if err != nil {
			return reflect.Value{}, BadRequest("body: " + err.Error())
		}
		seq := multipartIterator(r.Body, boundary)
		return reflect.ValueOf(seq).Convert(fb.iterType), nil
	}
	return reflect.Value{}, fmt.Errorf(
		"buildBodyIterator: unhandled kind %d",
		fb.kind,
	)
}

// adaptIterSeq2 wraps an iter.Seq2[any, error] into a reflect.Value of
// type iter.Seq2[itemType, error] by reflecting a typed yield-trampoline.
// We can't directly produce a generic iter.Seq2[T, error] at runtime since
// Go generics are erased, so we build the function value reflectively.
func adaptIterSeq2(
	seq iter.Seq2[any, error],
	iterType reflect.Type,
) reflect.Value {
	yieldType := iterType.In(0)
	return reflect.MakeFunc(
		iterType,
		func(args []reflect.Value) []reflect.Value {
			yield := args[0]
			seq(func(v any, err error) bool {
				var vv reflect.Value
				if v == nil {
					vv = reflect.Zero(yieldType.In(0))
				} else {
					vv = reflect.ValueOf(v)
					if vv.Type() != yieldType.In(0) {
						if vv.Type().ConvertibleTo(yieldType.In(0)) {
							vv = vv.Convert(yieldType.In(0))
						}
					}
				}
				var ev reflect.Value
				if err == nil {
					ev = reflect.Zero(errorType)
				} else {
					ev = reflect.ValueOf(err)
				}
				res := yield.Call([]reflect.Value{vv, ev})
				return res[0].Bool()
			})
			return nil
		},
	)
}

// jsonStreamIterator yields one decoded value per framed record. The framer
// (line for jsonl/ndjson, RS+line for json-seq/geo+json-seq) is picked by
// the caller from the request Content-Type so a single endpoint can accept
// any of the sequential JSON media types.
func jsonStreamIterator(
	body io.ReadCloser,
	itemType reflect.Type,
	codec Codec,
	framer streamFramer,
) iter.Seq2[any, error] {
	switch framer {
	case framerRS:
		return jsonSeqIterator(body, itemType, codec)
	default:
		return jsonlIterator(body, itemType, codec)
	}
}

func jsonlIterator(
	body io.ReadCloser,
	itemType reflect.Type,
	codec Codec,
) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 64*1024), 1<<24)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			vp := reflect.New(itemType)
			if err := codec.Decode(bytes.NewReader(line), vp.Interface()); err != nil {
				if !yield(reflect.Zero(itemType).Interface(), err) {
					return
				}
				continue
			}
			if !yield(vp.Elem().Interface(), nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(reflect.Zero(itemType).Interface(), err)
		}
	}
}

func jsonSeqIterator(
	body io.ReadCloser,
	itemType reflect.Type,
	codec Codec,
) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		defer body.Close()
		br := bufio.NewReader(body)
		for {
			b, err := br.ReadByte()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(reflect.Zero(itemType).Interface(), err)
				return
			}
			if b != recordSeparator {
				continue
			}
			record, err := readJSONSeqRecord(br)
			if err != nil {
				yield(reflect.Zero(itemType).Interface(), err)
				return
			}
			if len(bytes.TrimSpace(record)) == 0 {
				continue
			}
			vp := reflect.New(itemType)
			if decErr := codec.Decode(bytes.NewReader(record), vp.Interface()); decErr != nil {
				if !yield(reflect.Zero(itemType).Interface(), decErr) {
					return
				}
				continue
			}
			if !yield(vp.Elem().Interface(), nil) {
				return
			}
		}
	}
}

// readJSONSeqRecord reads bytes from br up to (but not including) the next
// 0x1E byte or EOF. The 0x1E delimiter byte is left in the stream for the
// next ReadByte call to consume — except on EOF where we just return.
func readJSONSeqRecord(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		b, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if b == recordSeparator {
			_ = br.UnreadByte()
			return buf.Bytes(), nil
		}
		buf.WriteByte(b)
	}
}

func sseIterator(body io.ReadCloser) iter.Seq2[SSEEvent, error] {
	return func(yield func(SSEEvent, error) bool) {
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 64*1024), 1<<24)
		var (
			ev     SSEEvent
			data   bytes.Buffer
			haveEv bool
		)
		flush := func() bool {
			if !haveEv {
				return true
			}
			ev.Data = strings.TrimSuffix(data.String(), "\n")
			ok := yield(ev, nil)
			ev = SSEEvent{}
			data.Reset()
			haveEv = false
			return ok
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // comment
			}
			haveEv = true
			name, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch name {
			case "event":
				ev.Event = value
			case "data":
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(value)
			case "id":
				ev.ID = value
			case "retry":
				if n, err := strconv.Atoi(value); err == nil {
					ev.Retry = n
				}
			}
		}
		if err := scanner.Err(); err != nil {
			yield(SSEEvent{}, err)
			return
		}
		flush()
	}
}

func multipartIterator(
	body io.ReadCloser,
	boundary string,
) iter.Seq2[Part, error] {
	return func(yield func(Part, error) bool) {
		defer body.Close()
		mr := multipart.NewReader(body, boundary)
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(Part{}, err)
				return
			}
			if !yield(Part{Header: p.Header, Body: p}, nil) {
				_ = p.Close()
				return
			}
			_ = p.Close()
		}
	}
}

func multipartBoundary(r *http.Request) (string, error) {
	ct := r.Header.Get("Content-Type")
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", fmt.Errorf("parsing Content-Type: %w", err)
	}
	b := params["boundary"]
	if b == "" {
		return "", fmt.Errorf("missing boundary parameter in Content-Type")
	}
	return b, nil
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
		allowed := splitContentTypes(f.Tag.Get("contentType"))
		kind, itemType, err := classifyBodyKind(f.Type, allowed)
		if err != nil {
			return fieldBinder{}, false, err
		}
		return fieldBinder{
			index:    idx,
			source:   srcBody,
			kind:     kind,
			allowed:  allowed,
			itemType: itemType,
			iterType: f.Type,
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

func classifyBodyKind(
	t reflect.Type,
	allowed []string,
) (bodyKind, reflect.Type, error) {
	if t == readerType {
		return bodyReader, nil, nil
	}
	if t == bytesType {
		return bodyBytes, nil, nil
	}
	if item, ok := iterSeq2ItemType(t); ok {
		kind, err := streamKindForContentTypes(allowed, item)
		if err != nil {
			return 0, nil, err
		}
		return kind, item, nil
	}
	// Anything else falls through to the codec (JSON by default).
	return bodyJSON, nil, nil
}

// iterSeq2ItemType returns T and true when t is iter.Seq2[T, error].
// Detects the underlying function signature func(yield func(T, error) bool)
// since reflect cannot directly express generic aliases.
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

// streamKindForContentTypes picks the right iterator bodyKind from the
// declared content types and validates the item type for SSE and multipart
// streams.
//
// All declared content types must agree on the yielded item type. Sequential
// JSON media types (jsonl, ndjson, json-seq, geo+json-seq) all yield T and
// can be mixed freely on one endpoint; SSE and multipart yield SSEEvent and
// Part respectively, so they cannot share an endpoint with anything else.
func streamKindForContentTypes(
	allowed []string,
	item reflect.Type,
) (bodyKind, error) {
	if len(allowed) == 0 {
		return 0, fmt.Errorf(
			"iter.Seq2 body requires a contentType:\"...\" tag",
		)
	}
	var kind bodyKind
	for i, ct := range allowed {
		k, err := bodyKindForContentType(ct)
		if err != nil {
			return 0, err
		}
		if i == 0 {
			kind = k
			continue
		}
		if k != kind {
			return 0, fmt.Errorf(
				"iter.Seq2 body: content types %q and %q yield "+
					"different item types and cannot share an endpoint",
				allowed[0], ct,
			)
		}
	}
	switch kind {
	case bodyIterSSE:
		if item != sseEventType {
			return 0, fmt.Errorf(
				"text/event-stream body must use iter.Seq2[router.SSEEvent, error], got iter.Seq2[%s, error]",
				item,
			)
		}
	case bodyIterMultipart:
		if item != partType {
			return 0, fmt.Errorf(
				"multipart body must use iter.Seq2[router.Part, error], got iter.Seq2[%s, error]",
				item,
			)
		}
	}
	return kind, nil
}

func bodyKindForContentType(ct string) (bodyKind, error) {
	switch normalizeMediaType(ct) {
	case "application/jsonl",
		"application/x-ndjson",
		"application/json-seq",
		"application/geo+json-seq":
		return bodyIterJSON, nil
	case "text/event-stream":
		return bodyIterSSE, nil
	case "multipart/mixed", "multipart/byteranges":
		return bodyIterMultipart, nil
	}
	return 0, fmt.Errorf(
		"iter.Seq2 body: unsupported streaming content type %q "+
			"(want application/jsonl, application/x-ndjson, "+
			"application/json-seq, application/geo+json-seq, "+
			"text/event-stream, or multipart/mixed)",
		ct,
	)
}

// jsonStreamFramer returns the right framer for one of the sequential JSON
// media types based on the actual request Content-Type header. Used by
// bodyIterJSON to dispatch at request time.
func jsonStreamFramer(ct string) streamFramer {
	switch normalizeMediaType(ct) {
	case "application/json-seq", "application/geo+json-seq":
		return framerRS
	}
	return framerLines
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
