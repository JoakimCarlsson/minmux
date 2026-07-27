package clientgen

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

type Block interface{ isBlock() }

type HeroBlock struct {
	Kind     string `json:"kind"     const:"hero"`
	Headline string `json:"headline"`
}

func (HeroBlock) isBlock() {}

type TextBlock struct {
	Kind string `json:"kind" const:"text"`
	Body string `json:"body"`
}

func (TextBlock) isBlock() {}

type Page struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

type Metrics struct {
	Counts  map[string]int     `json:"counts"`
	Nested  map[string]float64 `json:"nested,omitempty"`
	Payload json.RawMessage    `json:"payload,omitempty"`
}

func unionSpec() *openapi.Document {
	r := router.New()
	r.Get("/pages/{slug}", func(_ *router.Context, _ struct {
		Slug string `path:"slug"`
	}) {
	},
		openapi.OperationID("getPage"),
		openapi.ReturnsBody[Page](http.StatusOK, "A page"),
	)
	r.Get("/metrics", func(_ *router.Context) {},
		openapi.OperationID("getMetrics"),
		openapi.ReturnsBody[Metrics](http.StatusOK, "Metrics"),
	)
	gen := openapi.NewGenerator(openapi.Info{Title: "U", Version: "1"})
	gen.RegisterUnion((*Block)(nil), "kind", HeroBlock{}, TextBlock{})
	return gen.Spec(r)
}

// TestUnion_GeneratesTaggedWrapper covers discriminated unions end to end:
// the wrapper type, its discriminator-driven decode, and one typed
// accessor per variant.
func TestUnion_GeneratesTaggedWrapper(t *testing.T) {
	files, err := Render(Config{Spec: unionSpec(), Package: "unionclient"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	types := collapseSpaces(fileNamed(t, files, "types.gen.go"))

	for _, want := range []string{
		"func (u *PageBlocksItem) UnmarshalJSON(data []byte) error",
		"func (u PageBlocksItem) MarshalJSON() ([]byte, error)",
		"func (u PageBlocksItem) AsHeroBlock() (HeroBlock, bool)",
		"func (u PageBlocksItem) AsTextBlock() (TextBlock, bool)",
		`case "hero":`,
		`case "text":`,
		"Blocks []PageBlocksItem",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("missing %q in:\n%s", want, types)
		}
	}
}

// TestUnion_DecodesAtRuntime builds the generated package and exercises
// the emitted UnmarshalJSON, because a union whose dispatch is subtly
// wrong would still compile.
func TestUnion_DecodesAtRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(Config{
		Spec:    unionSpec(),
		Package: "unionclient",
		Out:     dir,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	writeModule(t, dir, "unionclient")

	probe := `package unionclient

import (
	"encoding/json"
	"testing"
)

func TestDispatch(t *testing.T) {
	var p Page
	raw := ` + "`" + `{"title":"home","blocks":[
		{"kind":"hero","headline":"hi"},
		{"kind":"text","body":"words"},
		{"kind":"future","whatever":1}
	]}` + "`" + `
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(p.Blocks) != 3 {
		t.Fatalf("blocks: %d", len(p.Blocks))
	}
	hero, ok := p.Blocks[0].AsHeroBlock()
	if !ok || hero.Headline != "hi" {
		t.Errorf("hero: %+v %v", hero, ok)
	}
	if _, ok := p.Blocks[0].AsTextBlock(); ok {
		t.Errorf("hero should not decode as text")
	}
	text, ok := p.Blocks[1].AsTextBlock()
	if !ok || text.Body != "words" {
		t.Errorf("text: %+v %v", text, ok)
	}
	if p.Blocks[2].Value != nil {
		t.Errorf("unknown variant should leave Value nil")
	}
	if p.Blocks[2].Tag != "future" {
		t.Errorf("unknown variant should keep its tag, got %q", p.Blocks[2].Tag)
	}
	out, err := json.Marshal(p.Blocks[2])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) == "null" {
		t.Errorf("unknown variant should round-trip through Raw")
	}
}
`
	path := filepath.Join(dir, "union_probe_test.go")
	if err := os.WriteFile(path, []byte(probe), 0o644); err != nil {
		t.Fatalf("writing probe: %v", err)
	}
	runGoTest(t, dir)
}

func runGoTest(t *testing.T, dir string) {
	t.Helper()
	cmd := goCommand(dir, "test", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated package failed its probe test: %v\n%s", err, out)
	}
}

func TestSchema_MapsAndRawMessages(t *testing.T) {
	files, err := Render(Config{Spec: unionSpec(), Package: "unionclient"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	types := collapseSpaces(fileNamed(t, files, "types.gen.go"))
	for _, want := range []string{
		"Counts map[string]int64 `json:\"counts\"`",
		"Nested map[string]float64 `json:\"nested,omitempty\"`",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("missing %q in:\n%s", want, types)
		}
	}
}

// collapseSpaces normalizes gofmt's column alignment so assertions can be
// written without mirroring the exact padding.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestBaseType_ScalarMapping(t *testing.T) {
	r := newRegistry()
	cases := []struct {
		name   string
		schema *openapi.Schema
		want   string
	}{
		{"string", &openapi.Schema{Type: "string"}, "string"},
		{
			"date-time",
			&openapi.Schema{Type: "string", Format: "date-time"},
			"time.Time",
		},
		{
			"binary",
			&openapi.Schema{Type: "string", Format: "binary"},
			"[]byte",
		},
		{"bool", &openapi.Schema{Type: "boolean"}, "bool"},
		{"int", &openapi.Schema{Type: "integer"}, "int"},
		{
			"int32",
			&openapi.Schema{Type: "integer", Format: "int32"},
			"int32",
		},
		{
			"int64",
			&openapi.Schema{Type: "integer", Format: "int64"},
			"int64",
		},
		{"double", &openapi.Schema{Type: "number"}, "float64"},
		{
			"float",
			&openapi.Schema{Type: "number", Format: "float"},
			"float32",
		},
		{
			"array",
			&openapi.Schema{
				Type:  "array",
				Items: &openapi.Schema{Type: "string"},
			},
			"[]string",
		},
		{
			"free-form object",
			&openapi.Schema{Type: "object"},
			"map[string]any",
		},
		{"nil schema", nil, "json.RawMessage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.baseType(tc.schema, "Hint")
			if err != nil {
				t.Fatalf("baseType: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGoType_PointerRule documents the one rule that decides whether a
// caller can distinguish "absent" from "zero".
func TestGoType_PointerRule(t *testing.T) {
	r := newRegistry()
	cases := []struct {
		name     string
		schema   *openapi.Schema
		required bool
		want     string
	}{
		{"required scalar", &openapi.Schema{Type: "string"}, true, "string"},
		{"optional scalar", &openapi.Schema{Type: "string"}, false, "*string"},
		{
			"optional slice stays nil-able",
			&openapi.Schema{
				Type:  "array",
				Items: &openapi.Schema{Type: "string"},
			},
			false,
			"[]string",
		},
		{
			"optional map stays nil-able",
			&openapi.Schema{
				Type:                 "object",
				AdditionalProperties: &openapi.Schema{Type: "string"},
			},
			false,
			"map[string]string",
		},
		{
			"optional timestamp",
			&openapi.Schema{Type: "string", Format: "date-time"},
			false,
			"*time.Time",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.goType(tc.schema, tc.required, "Hint")
			if err != nil {
				t.Fatalf("goType: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBaseType_UnresolvedRefIsAnError(t *testing.T) {
	r := newRegistry()
	_, err := r.baseType(&openapi.Schema{Ref: "#/components/schemas/Nope"}, "H")
	if err == nil || !strings.Contains(err.Error(), "unresolved $ref") {
		t.Errorf("want an unresolved-ref error, got %v", err)
	}
}
