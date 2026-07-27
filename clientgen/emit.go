package clientgen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

func (g *generator) renderTypes() []byte {
	var b bytes.Buffer
	for _, t := range g.reg.sortedTypes() {
		switch t.Kind {
		case kindStruct:
			g.writeStruct(&b, t)
		case kindUnion:
			g.writeUnion(&b, t)
		case kindAlias:
			writeDoc(&b, t.Name, t.Doc)
			fmt.Fprintf(&b, "type %s %s\n\n", t.Name, t.Alias)
		}
	}
	return g.renderFile(b.String())
}

func (g *generator) writeStruct(b *bytes.Buffer, t *namedType) {
	writeDoc(b, t.Name, t.Doc)
	fmt.Fprintf(b, "type %s struct {\n", t.Name)
	for i, f := range t.Fields {
		if f.Doc != "" {
			if i > 0 {
				b.WriteString("\n")
			}
			writeIndentedDoc(b, f.Name, f.Doc)
		}
		tag := f.JSONName
		if f.OmitEmpty {
			tag += ",omitempty"
		}
		fmt.Fprintf(b, "\t%s %s `json:%q`\n", f.Name, f.Type, tag)
	}
	b.WriteString("}\n\n")
}

// writeUnion emits a discriminated union as a tagged wrapper: the
// discriminator value, the decoded variant, and the original bytes.
//
// Keeping Raw means an unrecognized discriminator round-trips unchanged
// instead of being dropped, so a client compiled against an older spec
// stays forward-compatible with a server that added a variant.
func (g *generator) writeUnion(b *bytes.Buffer, t *namedType) {
	u := t.Union
	variantList := make([]string, 0, len(u.Variants))
	for _, v := range u.Variants {
		variantList = append(variantList, v.Type)
	}

	fmt.Fprintf(b,
		"// %s is a discriminated union keyed on the %q property.\n",
		t.Name, u.Discriminator,
	)
	fmt.Fprintf(b, "//\n// Variants: %s.\n", strings.Join(variantList, ", "))
	fmt.Fprintf(b, "type %s struct {\n", t.Name)
	fmt.Fprintf(b, "\t// Tag is the %q discriminator value.\n", u.Discriminator)
	b.WriteString("\tTag string\n\n")
	b.WriteString(
		"\t// Value is the decoded variant, or nil when Tag matched no\n",
	)
	b.WriteString("\t// known variant.\n")
	b.WriteString("\tValue any\n\n")
	b.WriteString(
		"\t// Raw is the original JSON, retained so an unknown variant\n",
	)
	b.WriteString("\t// survives a decode/encode round trip.\n")
	b.WriteString("\tRaw json.RawMessage\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// UnmarshalJSON decodes the variant selected by %q.\n",
		u.Discriminator)
	fmt.Fprintf(b, "func (u *%s) UnmarshalJSON(data []byte) error {\n", t.Name)
	b.WriteString("\tu.Raw = append(u.Raw[:0], data...)\n")
	b.WriteString("\tvar probe struct {\n")
	fmt.Fprintf(b, "\t\tTag string `json:%q`\n", u.Discriminator)
	b.WriteString("\t}\n")
	b.WriteString("\tif err := json.Unmarshal(data, &probe); err != nil {\n")
	fmt.Fprintf(b,
		"\t\treturn fmt.Errorf(\"decoding %s discriminator: %%w\", err)\n",
		t.Name,
	)
	b.WriteString("\t}\n")
	b.WriteString("\tu.Tag = probe.Tag\n")
	b.WriteString("\tswitch probe.Tag {\n")
	for _, v := range u.Variants {
		fmt.Fprintf(b, "\tcase %q:\n", v.Value)
		fmt.Fprintf(b, "\t\tvar v %s\n", v.Type)
		b.WriteString("\t\tif err := json.Unmarshal(data, &v); err != nil {\n")
		fmt.Fprintf(b,
			"\t\t\treturn fmt.Errorf(\"decoding %s variant %s: %%w\", err)\n",
			t.Name, v.Value,
		)
		b.WriteString("\t\t}\n")
		b.WriteString("\t\tu.Value = v\n")
		b.WriteString("\t\treturn nil\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("\tu.Value = nil\n")
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(b,
		"// MarshalJSON encodes the decoded variant, falling back to the\n"+
			"// original bytes when the variant was not recognized.\n")
	fmt.Fprintf(b, "func (u %s) MarshalJSON() ([]byte, error) {\n", t.Name)
	b.WriteString("\tif u.Value != nil {\n")
	b.WriteString("\t\treturn json.Marshal(u.Value)\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif len(u.Raw) > 0 {\n")
	b.WriteString("\t\treturn u.Raw, nil\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn []byte(\"null\"), nil\n")
	b.WriteString("}\n\n")

	for _, v := range u.Variants {
		fmt.Fprintf(b,
			"// As%s returns the value as a %s, reporting false when the\n"+
				"// union holds a different variant.\n",
			v.Type, v.Type,
		)
		fmt.Fprintf(b, "func (u %s) As%s() (%s, bool) {\n",
			t.Name, v.Type, v.Type)
		fmt.Fprintf(b, "\tv, ok := u.Value.(%s)\n", v.Type)
		b.WriteString("\treturn v, ok\n")
		b.WriteString("}\n\n")
	}
}

func (g *generator) renderOperations() []byte {
	var b bytes.Buffer
	for _, op := range g.ops {
		g.writeParamsStruct(&b, op)
	}
	for _, op := range g.ops {
		g.writeMethod(&b, op)
	}
	return g.renderFile(b.String())
}

func (g *generator) writeParamsStruct(b *bytes.Buffer, op *operation) {
	if !op.HasInput() {
		return
	}
	fmt.Fprintf(b, "// %s is the input to %s.\n", op.ParamsType, op.Name)
	fmt.Fprintf(b, "type %s struct {\n", op.ParamsType)
	first := true
	for _, p := range op.Params {
		if !first {
			b.WriteString("\n")
		}
		first = false
		writeParamDoc(b, p)
		fmt.Fprintf(b, "\t%s %s\n", p.Field, p.Type)
	}
	if op.Body != nil {
		if !first {
			b.WriteString("\n")
		}
		g.writeBodyFields(b, op.Body)
	}
	b.WriteString("}\n\n")
}

func (g *generator) writeBodyFields(b *bytes.Buffer, body *bodySpec) {
	switch body.Kind {
	case bodyJSON:
		b.WriteString("\t// Body is the JSON request body.\n")
		fmt.Fprintf(b, "\tBody %s\n", body.Type)
	case bodyBinary:
		fmt.Fprintf(b,
			"\t// Body is the raw %s request body.\n", body.ContentType)
		b.WriteString("\tBody io.Reader\n")
	case bodyJSONStream:
		fmt.Fprintf(b,
			"\t// Body streams the request as %s. It is consumed once, on a\n"+
				"\t// background goroutine, so the whole sequence is never\n"+
				"\t// buffered in memory.\n",
			body.ContentType,
		)
		fmt.Fprintf(b, "\tBody iter.Seq2[%s, error]\n", body.ItemType)
	case bodyURLEncoded, bodyMultipart:
		first := true
		for _, f := range body.Fields {
			if !first {
				b.WriteString("\n")
			}
			first = false
			fmt.Fprintf(b, "\t// %s is the %q form field.\n", f.Field, f.Wire)
			fmt.Fprintf(b, "\t%s %s\n", f.Field, f.Type)
		}
		for _, f := range body.Files {
			if !first {
				b.WriteString("\n")
			}
			first = false
			fmt.Fprintf(b, "\t// %s is the %q file part.\n", f.Field, f.Wire)
			fmt.Fprintf(b, "\t%s %s\n", f.Field, f.Type)
		}
	}
}

// returnType is the Go type an operation's method returns alongside error,
// or "" when the operation has no success body.
func (op *operation) returnType() string {
	if op.Stream != nil {
		switch op.Stream.Kind {
		case streamJSON:
			return "iter.Seq2[" + op.Stream.ItemType + ", error]"
		case streamSSE:
			return "iter.Seq2[SSEEvent, error]"
		case streamMultipart:
			return "iter.Seq2[Part, error]"
		}
	}
	return op.Success.Type
}

func (g *generator) writeMethod(b *bytes.Buffer, op *operation) {
	ret := op.returnType()
	g.writeMethodDoc(b, op, ret)

	sig := fmt.Sprintf("func (c *%s) %s(ctx context.Context",
		g.clientType(), op.Name)
	if op.HasInput() {
		sig += ", p " + op.ParamsType
	}
	if ret == "" {
		sig += ") error {\n"
	} else {
		sig += ") (" + ret + ", error) {\n"
	}
	b.WriteString(sig)

	fail := "return "
	if ret != "" {
		if op.Stream != nil {
			fail = "return nil, "
		} else {
			fmt.Fprintf(b, "\tvar zero %s\n", ret)
			fail = "return zero, "
		}
	}

	g.writePathBuild(b, op)
	g.writeQueryBuild(b, op)
	g.writeHeaderBuild(b, op)
	g.writeBodyBuild(b, op, fail)
	g.writeRequestBuild(b, op, fail)

	b.WriteString("\tresp, err := c.do(req)\n")
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\t%serr\n", fail)
	b.WriteString("\t}\n")

	if op.Stream != nil {
		g.writeStreamDispatch(b, op)
	} else {
		g.writeResponseDispatch(b, op, ret, fail)
	}
	b.WriteString("}\n\n")
}

func (g *generator) writeMethodDoc(
	b *bytes.Buffer,
	op *operation,
	ret string,
) {
	summary := op.Summary
	if summary == "" {
		summary = fmt.Sprintf("issues %s %s.", op.Method, op.Path)
	} else {
		summary = lowerFirstWord(summary)
	}
	fmt.Fprintf(b, "// %s %s\n", op.Name, summary)
	fmt.Fprintf(b, "//\n// %s %s\n", op.Method, op.Path)
	if op.Description != "" {
		b.WriteString("//\n")
		writeCommentBlock(b, op.Description, "")
	}
	if op.Stream != nil {
		b.WriteString("//\n")
		b.WriteString(
			"// The returned sequence owns the response body and closes it\n" +
				"// when iteration ends, including on an early break. A caller\n" +
				"// that never iterates leaks the connection.\n")
	}
	if len(op.Extra2xx) > 0 {
		b.WriteString("//\n")
		codes := make([]string, 0, len(op.Extra2xx))
		for _, c := range op.Extra2xx {
			codes = append(codes, strconv.Itoa(c))
		}
		fmt.Fprintf(b,
			"// The spec also declares success status %s with a different\n"+
				"// body type, which a single typed return cannot express; such a\n"+
				"// response yields the zero value and a nil error.\n",
			strings.Join(codes, ", "),
		)
	}
	if ret == "" && op.Stream == nil {
		b.WriteString("//\n// The operation declares no success body.\n")
	}
	if op.Deprecated {
		b.WriteString(
			"//\n// Deprecated: this operation is marked deprecated in the API spec.\n",
		)
	}
}

func (g *generator) writePathBuild(b *bytes.Buffer, op *operation) {
	if len(op.PathParts) == 1 && op.PathParts[0].Field == "" {
		fmt.Fprintf(b, "\tpath := %q\n", op.PathParts[0].Literal)
		return
	}
	b.WriteString("\tpath := ")
	for i, part := range op.PathParts {
		if i > 0 {
			b.WriteString(" + ")
		}
		if part.Field == "" {
			fmt.Fprintf(b, "%q", part.Literal)
			continue
		}
		helper := "pathParam"
		if part.Wildcard {
			helper = "wildcardParam"
		}
		fmt.Fprintf(b, "%s(formatValue(p.%s))", helper, part.Field)
	}
	b.WriteString("\n")
}

func (g *generator) writeQueryBuild(b *bytes.Buffer, op *operation) {
	params := paramsIn(op, "query")
	if len(params) == 0 {
		return
	}
	b.WriteString("\tquery := url.Values{}\n")
	for _, p := range params {
		writeValueSet(b, p, "query", "Add")
	}
}

func (g *generator) writeHeaderBuild(b *bytes.Buffer, op *operation) {
	params := paramsIn(op, "header")
	if len(params) == 0 {
		return
	}
	b.WriteString("\theader := http.Header{}\n")
	for _, p := range params {
		writeValueSet(b, p, "header", "Add")
	}
}

// writeValueSet emits the assignment for one query or header parameter.
// An optional parameter is skipped entirely when nil so the server sees
// "absent" rather than an empty string, which is the distinction the
// minmux binder relies on.
func writeValueSet(b *bytes.Buffer, p opParam, target, addMethod string) {
	switch {
	case p.IsSlice:
		fmt.Fprintf(b, "\tfor _, v := range p.%s {\n", p.Field)
		fmt.Fprintf(b, "\t\t%s.%s(%q, formatValue(v))\n",
			target, addMethod, p.Wire)
		b.WriteString("\t}\n")
	case p.Required:
		fmt.Fprintf(b, "\t%s.Set(%q, formatValue(p.%s))\n",
			target, p.Wire, p.Field)
	case strings.HasPrefix(p.Type, "*"):
		fmt.Fprintf(b, "\tif p.%s != nil {\n", p.Field)
		fmt.Fprintf(b, "\t\t%s.Set(%q, formatValue(*p.%s))\n",
			target, p.Wire, p.Field)
		b.WriteString("\t}\n")
	default:
		fmt.Fprintf(b, "\t%s.Set(%q, formatValue(p.%s))\n",
			target, p.Wire, p.Field)
	}
}

func paramsIn(op *operation, in string) []opParam {
	var out []opParam
	for _, p := range op.Params {
		if p.In == in {
			out = append(out, p)
		}
	}
	return out
}

func (g *generator) writeBodyBuild(
	b *bytes.Buffer,
	op *operation,
	fail string,
) {
	if op.Body == nil {
		return
	}
	switch op.Body.Kind {
	case bodyJSON:
		b.WriteString("\tbody, err := jsonBody(p.Body)\n")
		b.WriteString("\tif err != nil {\n")
		fmt.Fprintf(b, "\t\t%serr\n", fail)
		b.WriteString("\t}\n")
	case bodyBinary:
		b.WriteString("\tbody := p.Body\n")
	case bodyJSONStream:
		framer := "framerLines"
		if op.Body.FramerRS {
			framer = "framerRS"
		}
		fmt.Fprintf(b, "\tbody := jsonLinesBody(p.Body, %s)\n", framer)
	case bodyURLEncoded:
		b.WriteString("\tform := url.Values{}\n")
		for _, f := range op.Body.Fields {
			writeFormSet(b, f)
		}
		b.WriteString("\tbody := strings.NewReader(form.Encode())\n")
	case bodyMultipart:
		b.WriteString("\tform := url.Values{}\n")
		for _, f := range op.Body.Fields {
			writeFormSet(b, f)
		}
		b.WriteString("\tfiles := map[string][]FormFile{}\n")
		for _, f := range op.Body.Files {
			writeFileSet(b, f)
		}
		b.WriteString(
			"\tbody, contentType, err := multipartBody(form, files)\n",
		)
		b.WriteString("\tif err != nil {\n")
		fmt.Fprintf(b, "\t\t%serr\n", fail)
		b.WriteString("\t}\n")
	}
}

func writeFormSet(b *bytes.Buffer, f formField) {
	switch {
	case f.IsSlice:
		fmt.Fprintf(b, "\tfor _, v := range p.%s {\n", f.Field)
		fmt.Fprintf(b, "\t\tform.Add(%q, formatValue(v))\n", f.Wire)
		b.WriteString("\t}\n")
	case strings.HasPrefix(f.Type, "*"):
		fmt.Fprintf(b, "\tif p.%s != nil {\n", f.Field)
		fmt.Fprintf(
			b,
			"\t\tform.Set(%q, formatValue(*p.%s))\n",
			f.Wire,
			f.Field,
		)
		b.WriteString("\t}\n")
	default:
		fmt.Fprintf(b, "\tform.Set(%q, formatValue(p.%s))\n", f.Wire, f.Field)
	}
}

func writeFileSet(b *bytes.Buffer, f formField) {
	if f.IsSlice {
		fmt.Fprintf(b, "\tif len(p.%s) > 0 {\n", f.Field)
		fmt.Fprintf(b, "\t\tfiles[%q] = p.%s\n", f.Wire, f.Field)
		b.WriteString("\t}\n")
		return
	}
	fmt.Fprintf(b, "\tif p.%s != nil {\n", f.Field)
	fmt.Fprintf(b, "\t\tfiles[%q] = []FormFile{*p.%s}\n", f.Wire, f.Field)
	b.WriteString("\t}\n")
}

func (g *generator) writeRequestBuild(
	b *bytes.Buffer,
	op *operation,
	fail string,
) {
	b.WriteString("\treq, err := c.newRequest(ctx, request{\n")
	fmt.Fprintf(b, "\t\tmethod: %q,\n", op.Method)
	b.WriteString("\t\tpath:   path,\n")
	if len(paramsIn(op, "query")) > 0 {
		b.WriteString("\t\tquery:  query,\n")
	}
	if len(paramsIn(op, "header")) > 0 {
		b.WriteString("\t\theader: header,\n")
	}
	if op.Body != nil {
		b.WriteString("\t\tbody:   body,\n")
		if op.Body.Kind == bodyMultipart {
			b.WriteString("\t\tcontentType: contentType,\n")
		} else {
			fmt.Fprintf(b, "\t\tcontentType: %q,\n", op.Body.ContentType)
		}
	}
	if op.Accept != "" {
		fmt.Fprintf(b, "\t\taccept: %q,\n", op.Accept)
	}
	b.WriteString("\t})\n")
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\t%serr\n", fail)
	b.WriteString("\t}\n")
}

func (g *generator) writeResponseDispatch(
	b *bytes.Buffer,
	op *operation,
	ret, fail string,
) {
	b.WriteString("\trespBody, err := readBody(resp)\n")
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\t%serr\n", fail)
	b.WriteString("\t}\n")
	b.WriteString("\tswitch resp.StatusCode {\n")

	if op.Success.Status != 0 {
		fmt.Fprintf(b, "\tcase %d:\n", op.Success.Status)
		if ret == "" {
			b.WriteString("\t\treturn nil\n")
		} else {
			fmt.Fprintf(b, "\t\tvar out %s\n", ret)
			b.WriteString(
				"\t\tif err := decodeJSON(respBody, &out); err != nil {\n",
			)
			fmt.Fprintf(b, "\t\t\t%serr\n", fail)
			b.WriteString("\t\t}\n")
			b.WriteString("\t\treturn out, nil\n")
		}
	}
	for _, code := range op.Extra2xx {
		fmt.Fprintf(b, "\tcase %d:\n", code)
		if ret == "" {
			b.WriteString("\t\treturn nil\n")
		} else {
			b.WriteString("\t\treturn zero, nil\n")
		}
	}
	for _, e := range op.Errors {
		fmt.Fprintf(b, "\tcase %d:\n", e.Status)
		writeErrorReturn(
			b,
			e.Type,
			fail,
			"newAPIError(req, resp, respBody, %s)",
		)
	}
	b.WriteString("\t}\n")

	if op.Success.Status == 0 {
		b.WriteString(
			"\tif resp.StatusCode >= 200 && resp.StatusCode < 300 {\n",
		)
		if ret == "" {
			b.WriteString("\t\treturn nil\n")
		} else {
			fmt.Fprintf(b, "\t\tvar out %s\n", ret)
			b.WriteString(
				"\t\tif err := decodeJSON(respBody, &out); err != nil {\n",
			)
			fmt.Fprintf(b, "\t\t\t%serr\n", fail)
			b.WriteString("\t\t}\n")
			b.WriteString("\t\treturn out, nil\n")
		}
		b.WriteString("\t}\n")
	}

	defaultType := ""
	if op.Default != nil {
		defaultType = op.Default.Type
	}
	writeErrorReturn(
		b,
		defaultType,
		fail,
		"newAPIError(req, resp, respBody, %s)",
	)
}

func (g *generator) writeStreamDispatch(b *bytes.Buffer, op *operation) {
	fail := "return nil, "
	b.WriteString("\tswitch resp.StatusCode {\n")
	fmt.Fprintf(b, "\tcase %d:\n", op.Success.Status)
	switch op.Stream.Kind {
	case streamJSON:
		fmt.Fprintf(b, "\t\treturn jsonStreamSeq[%s](resp), nil\n",
			op.Stream.ItemType)
	case streamSSE:
		b.WriteString("\t\treturn sseSeq(resp.Body), nil\n")
	case streamMultipart:
		b.WriteString("\t\treturn multipartSeq(resp), nil\n")
	}
	for _, e := range op.Errors {
		fmt.Fprintf(b, "\tcase %d:\n", e.Status)
		writeErrorReturn(b, e.Type, fail, "streamError(req, resp, %s)")
	}
	b.WriteString("\t}\n")

	defaultType := ""
	if op.Default != nil {
		defaultType = op.Default.Type
	}
	writeErrorReturn(b, defaultType, fail, "streamError(req, resp, %s)")
}

// writeErrorReturn emits the return of an *APIError, declaring a typed
// destination first when the spec says what the body decodes into.
func writeErrorReturn(b *bytes.Buffer, typ, fail, callFmt string) {
	if typ == "" {
		fmt.Fprintf(b, "\t%s%s\n", fail, fmt.Sprintf(callFmt, "nil"))
		return
	}
	fmt.Fprintf(b, "\tvar v %s\n", typ)
	fmt.Fprintf(b, "\t%s%s\n", fail, fmt.Sprintf(callFmt, "&v"))
}

func writeDoc(b *bytes.Buffer, name, doc string) {
	if doc == "" {
		fmt.Fprintf(b, "// %s is a schema defined by the API.\n", name)
		return
	}
	writeCommentBlock(b, name+" "+lowerFirstWord(doc), "")
}

func writeIndentedDoc(b *bytes.Buffer, name, doc string) {
	writeCommentBlock(b, name+" "+lowerFirstWord(doc), "\t")
}

func writeParamDoc(b *bytes.Buffer, p opParam) {
	desc := p.Doc
	if desc == "" {
		desc = fmt.Sprintf("is the %q %s parameter.", p.Wire, p.In)
	} else {
		desc = lowerFirstWord(desc)
	}
	writeCommentBlock(b, p.Field+" "+desc, "\t")
	if !p.Required {
		fmt.Fprintf(b, "\t// It is optional; a nil value omits it.\n")
	}
	if p.Deprecated {
		fmt.Fprintf(
			b,
			"\t//\n\t// Deprecated: this parameter is marked deprecated in the API spec.\n",
		)
	}
}

// writeCommentBlock wraps text into a Go comment block at roughly 72
// columns so generated documentation stays inside the repository's line
// budget.
func writeCommentBlock(b *bytes.Buffer, text, indent string) {
	const width = 68
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			fmt.Fprintf(b, "%s//\n", indent)
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			if line == "" {
				line = word
				continue
			}
			if len(line)+1+len(word) > width {
				fmt.Fprintf(b, "%s// %s\n", indent, line)
				line = word
				continue
			}
			line += " " + word
		}
		if line != "" {
			fmt.Fprintf(b, "%s// %s\n", indent, line)
		}
	}
}

// lowerFirstWord lowercases the first rune of a sentence so it reads
// naturally after a Go doc comment's leading identifier, unless the word
// looks like an acronym or proper noun already.
func lowerFirstWord(s string) string {
	if s == "" {
		return s
	}
	fields := strings.Fields(s)
	first := fields[0]
	if len(first) > 1 && first == strings.ToUpper(first) {
		return s
	}
	runes := []rune(s)
	lowered := strings.ToLower(string(runes[0]))
	return lowered + string(runes[1:])
}
