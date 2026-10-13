package clientgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func render(t *testing.T, pkg string) []File {
	t.Helper()
	files, err := Render(Config{Spec: fixtureSpec(), Package: pkg})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return files
}

func fileNamed(t *testing.T, files []File, name string) string {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return string(f.Content)
		}
	}
	t.Fatalf("no generated file named %q", name)
	return ""
}

// TestRender_GeneratedPackageCompiles is the load-bearing test: it builds
// the emitted client with the real Go toolchain in a throwaway module. A
// generator whose output does not compile is worthless no matter how many
// substring assertions pass.
func TestRender_GeneratedPackageCompiles(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(Config{
		Spec:    fixtureSpec(),
		Package: "fixtureclient",
		Out:     dir,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	writeModule(t, dir, "fixtureclient")
	buildModule(t, dir)
}

// TestRender_CustomClientNameCompiles covers the ClientName rewrite of the
// embedded runtime, which is string-substitution based and would otherwise
// only be exercised at its default value.
func TestRender_CustomClientNameCompiles(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(Config{
		Spec:       fixtureSpec(),
		Package:    "todoapi",
		ClientName: "TodoClient",
		Out:        dir,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	writeModule(t, dir, "todoapi")
	buildModule(t, dir)

	src, err := os.ReadFile(filepath.Join(dir, "operations.gen.go"))
	if err != nil {
		t.Fatalf("reading operations: %v", err)
	}
	if !strings.Contains(string(src), "func (c *TodoClient) GetApiV1Todos(") {
		t.Errorf("methods not bound to the renamed client type")
	}
}

func writeModule(t *testing.T, dir, pkg string) {
	t.Helper()
	gomod := "module example.com/" + pkg + "\n\ngo 1.26.0\n"
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644)
	if err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
}

// goCommand runs the Go toolchain against a throwaway module, with the
// repository workspace switched off so the temporary directory is not
// pulled into it.
func goCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	return cmd
}

func buildModule(t *testing.T, dir string) {
	t.Helper()
	out, err := goCommand(dir, "build", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("generated package does not compile: %v\n%s", err, out)
	}
}

// TestRender_Deterministic guards the CI drift check: the same spec must
// produce byte-identical output every run, or `go generate && git diff
// --exit-code` becomes a coin flip.
func TestRender_Deterministic(t *testing.T) {
	for i := range 5 {
		first := render(t, "fixtureclient")
		second := render(t, "fixtureclient")
		if len(first) != len(second) {
			t.Fatalf("run %d: file count differs", i)
		}
		for j := range first {
			if first[j].Name != second[j].Name {
				t.Fatalf("run %d: file %d name differs", i, j)
			}
			if !bytes.Equal(first[j].Content, second[j].Content) {
				t.Errorf("run %d: %s differs between renders", i, first[j].Name)
			}
		}
	}
}

// TestRender_RequiredDrivesPointers pins the behavior the openapi
// `required` array exists to enable: a partial-update body keeps its
// pointers so "leave unchanged" stays expressible, while a response type
// the server always fills stays free of them.
func TestRender_RequiredDrivesPointers(t *testing.T) {
	types := fileNamed(t, render(t, "fixtureclient"), "types.gen.go")

	for _, want := range []string{
		"Completed *bool   `json:\"completed,omitempty\"`",
		"Title     *string `json:\"title,omitempty\"`",
	} {
		if !strings.Contains(types, want) {
			t.Errorf(
				"UpdateTodoCommand missing optional field %q\n%s",
				want,
				types,
			)
		}
	}
	for _, want := range []string{
		"Completed bool      `json:\"completed\"`",
		"CreatedAt time.Time `json:\"created_at\"`",
		"Title     string    `json:\"title\"`",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("Todo missing required field %q\n%s", want, types)
		}
	}
	if strings.Contains(types, "Tags      []string  `json:\"tags\"`") {
		t.Errorf("omitempty slice should keep its omitempty tag")
	}
}

func TestRender_StreamingReturnTypes(t *testing.T) {
	ops := fileNamed(t, render(t, "fixtureclient"), "operations.gen.go")
	cases := map[string]string{
		"sequential JSON": "GetProgress(ctx context.Context) " +
			"(iter.Seq2[Progress, error], error)",
		"server-sent events": "GetEvents(ctx context.Context) " +
			"(iter.Seq2[SSEEvent, error], error)",
	}
	for name, want := range cases {
		if !strings.Contains(ops, want) {
			t.Errorf("%s: missing signature %q", name, want)
		}
	}
	if !strings.Contains(ops, "return jsonStreamSeq[Progress](resp), nil") {
		t.Errorf("sequential stream should decode through jsonStreamSeq")
	}
	if !strings.Contains(ops, "return sseSeq(resp.Body), nil") {
		t.Errorf("SSE stream should decode through sseSeq")
	}
}

// TestRender_ErrorBodiesAreTyped checks that a declared non-success status
// decodes into the type the spec names rather than being flattened to raw
// bytes.
func TestRender_ErrorBodiesAreTyped(t *testing.T) {
	ops := fileNamed(t, render(t, "fixtureclient"), "operations.gen.go")
	want := "\tcase 404:\n\t\tvar v ProblemDetails\n" +
		"\t\treturn zero, newAPIError(req, resp, respBody, &v)"
	if !strings.Contains(ops, want) {
		t.Errorf("404 should decode into ProblemDetails, got:\n%s", ops)
	}
	if !strings.Contains(
		ops,
		"return zero, newAPIError(req, resp, respBody, nil)",
	) {
		t.Errorf("undeclared statuses should still produce an APIError")
	}
	if !strings.Contains(ops, "return nil, streamError(req, resp, &v)") {
		t.Errorf("streaming operations should report typed errors too")
	}
}

func TestRender_PathAndParameterEncoding(t *testing.T) {
	ops := fileNamed(t, render(t, "fixtureclient"), "operations.gen.go")
	cases := map[string]string{
		"path parameter": `path := "/api/v1/todos/" + pathParam(formatValue(p.ID))`,
		"wildcard":       `path := "/files/" + wildcardParam(formatValue(p.Path))`,
		"required query": `query.Set("limit", formatValue(p.Limit))`,
		"optional query": "if p.Completed != nil {",
		"header":         `header.Set("X-Trace-Id", formatValue(*p.XTraceID))`,
		"multipart":      "body, contentType, err := multipartBody(form, files)",
	}
	for name, want := range cases {
		if !strings.Contains(ops, want) {
			t.Errorf("%s: missing %q", name, want)
		}
	}
}

// TestRender_AcceptsBodyTypesARawBody checks that a raw body declared with
// AcceptsBody is sent as the declared type, and that its json.RawMessage
// field survives as json.RawMessage.
func TestRender_AcceptsBodyTypesARawBody(t *testing.T) {
	files := render(t, "fixtureclient")
	ops := fileNamed(t, files, "operations.gen.go")
	types := fileNamed(t, files, "types.gen.go")

	params := "type PostWebhooksParams struct {\n" +
		"\t// Body is the JSON request body.\n\tBody WebhookEvent\n}"
	if !strings.Contains(ops, params) {
		t.Errorf("missing %q\n%s", params, ops)
	}
	_, method, ok := strings.Cut(ops, "func (c *Client) PostWebhooks(")
	if !ok {
		t.Fatalf("no PostWebhooks method\n%s", ops)
	}
	method, _, _ = strings.Cut(method, "\n}\n")
	for _, want := range []string{
		"body, err := jsonBody(p.Body)",
		`contentType: "application/json",`,
	} {
		if !strings.Contains(method, want) {
			t.Errorf("PostWebhooks missing %q\n%s", want, method)
		}
	}
	want := "Payload json.RawMessage `json:\"payload\"`"
	if !strings.Contains(types, want) {
		t.Errorf("WebhookEvent missing %q\n%s", want, types)
	}
}

func TestGenerate_WritesFilesAndPrunesStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "leftover.gen.go")
	if err := os.WriteFile(stale, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("seeding stale file: %v", err)
	}
	keep := filepath.Join(dir, "handwritten.go")
	if err := os.WriteFile(keep, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("seeding hand-written file: %v", err)
	}

	err := Generate(Config{
		Spec:    fixtureSpec(),
		Package: "fixtureclient",
		Out:     dir,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, name := range generatedFileNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to be written: %v", name, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale .gen.go file should have been pruned")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("hand-written file should be left alone: %v", err)
	}
}

func TestGenerate_RerunIsAByteForByteNoop(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Spec: fixtureSpec(), Package: "fixtureclient", Out: dir}
	if err := Generate(cfg); err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	before := readAll(t, dir)
	if err := Generate(cfg); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	after := readAll(t, dir)

	if len(before) != len(after) {
		t.Fatalf("file set changed on regeneration")
	}
	for name, content := range before {
		if !bytes.Equal(content, after[name]) {
			t.Errorf("%s changed on regeneration of an unchanged spec", name)
		}
	}
}

func readAll(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		out[e.Name()] = data
	}
	return out
}

func TestConfig_Validation(t *testing.T) {
	spec := fixtureSpec()
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing spec", Config{Package: "p"}, "Spec is required"},
		{"missing package", Config{Spec: spec}, "Package is required"},
		{
			"invalid package",
			Config{Spec: spec, Package: "not a package"},
			"not a valid Go package name",
		},
		{
			"keyword package",
			Config{Spec: spec, Package: "range"},
			"not a valid Go package name",
		},
		{
			"invalid client name",
			Config{Spec: spec, Package: "p", ClientName: "1Bad"},
			"not a valid Go identifier",
		},
		{
			"multiline command",
			Config{Spec: spec, Package: "p", Command: "a\nb"},
			"must be a single line",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render(tc.cfg)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestGenerate_RequiresOutputDirectory(t *testing.T) {
	err := Generate(Config{Spec: fixtureSpec(), Package: "p"})
	if err == nil || !strings.Contains(err.Error(), "Out is required") {
		t.Errorf("want Out-required error, got %v", err)
	}
}

func TestRender_HeaderNamesTheCommand(t *testing.T) {
	files, err := Render(Config{
		Spec:    fixtureSpec(),
		Package: "fixtureclient",
		Command: "go run ./gen",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, f := range files {
		want := "// Code generated by go run ./gen. DO NOT EDIT.\n"
		if !strings.HasPrefix(string(f.Content), want) {
			t.Errorf("%s: missing generated header", f.Name)
		}
	}
}

func TestImportsFor_OnlyIncludesReferencedPackages(t *testing.T) {
	body := "func f(ctx context.Context) time.Time { return time.Now() }"
	got := importsFor(body)
	want := []string{"context", "time"}
	if !slices.Equal(got, want) {
		t.Errorf("importsFor: want %v, got %v", want, got)
	}
	if slices.Contains(importsFor("func f() {}"), "net/http") {
		t.Errorf("unreferenced packages should not be imported")
	}
}
