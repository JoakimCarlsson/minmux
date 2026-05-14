package openapi

import (
	"iter"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/joakimcarlsson/minmux/router"
)

type Token struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

func TestSpec_StreamsBodyEmitsItemSchema(t *testing.T) {
	r := router.New()
	r.Get("/logs", noop,
		StreamsBody[LogEntry](
			http.StatusOK,
			"log entries",
			"application/jsonl",
			"application/x-ndjson",
			"application/json-seq",
		),
	)
	op := operation(t, r, "/logs", "GET")

	resp := op.Responses["200"]
	if resp == nil {
		t.Fatalf("missing 200 response: %v", op.Responses)
	}
	keys := contentKeys(resp.Content)
	sort.Strings(keys)
	want := []string{
		"application/json-seq",
		"application/jsonl",
		"application/x-ndjson",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("content keys: want %v, got %v", want, keys)
	}
	for _, k := range keys {
		mt := resp.Content[k]
		if mt.Schema != nil {
			t.Errorf(
				"%s: schema should be empty when itemSchema is set, got %+v",
				k,
				mt.Schema,
			)
		}
		if mt.ItemSchema == nil {
			t.Fatalf("%s: missing itemSchema", k)
		}
		if mt.ItemSchema.Ref != "#/components/schemas/LogEntry" {
			t.Errorf("%s itemSchema: want $ref to LogEntry, got %+v",
				k, mt.ItemSchema)
		}
	}
}

func TestSpec_StreamsBodyDefaultContentType(t *testing.T) {
	r := router.New()
	r.Get("/logs", noop, StreamsBody[LogEntry](http.StatusOK, ""))
	op := operation(t, r, "/logs", "GET")
	if _, ok := op.Responses["200"].Content["application/jsonl"]; !ok {
		t.Fatalf("default content type should be application/jsonl, got %v",
			contentKeys(op.Responses["200"].Content))
	}
}

func TestSpec_SSEStreamCanonicalEventSchema(t *testing.T) {
	r := router.New()
	r.Get("/tokens", noop, SSEStream[Token](http.StatusOK, "tokens"))
	op := operation(t, r, "/tokens", "GET")

	resp := op.Responses["200"]
	mt, ok := resp.Content["text/event-stream"]
	if !ok {
		t.Fatalf("missing text/event-stream, got %v",
			contentKeys(resp.Content))
	}
	if mt.ItemSchema == nil {
		t.Fatal("missing itemSchema for SSE")
	}
	if mt.ItemSchema.Type != "object" {
		t.Errorf("event schema type: %q", mt.ItemSchema.Type)
	}
	if len(mt.ItemSchema.Required) != 1 || mt.ItemSchema.Required[0] != "data" {
		t.Errorf(
			"event schema required: want [data], got %v",
			mt.ItemSchema.Required,
		)
	}
	props := mt.ItemSchema.Properties
	for _, name := range []string{"data", "event", "id", "retry"} {
		if _, ok := props[name]; !ok {
			t.Errorf("missing event property %q", name)
		}
	}
	if props["retry"].Type != "integer" {
		t.Errorf("retry type: %+v", props["retry"])
	}
	if props["retry"].Minimum == nil || *props["retry"].Minimum != 0 {
		t.Errorf("retry minimum: want 0, got %+v", props["retry"].Minimum)
	}
	data := props["data"]
	if data.Type != "string" {
		t.Errorf("data type: %q", data.Type)
	}
	if data.ContentMediaType != "application/json" {
		t.Errorf("data contentMediaType: %q", data.ContentMediaType)
	}
	if data.ContentSchema == nil ||
		data.ContentSchema.Ref != "#/components/schemas/Token" {
		t.Errorf("data contentSchema: want $ref to Token, got %+v",
			data.ContentSchema)
	}
}

func TestSpec_SSEStreamWithoutJSONPayload(t *testing.T) {
	r := router.New()
	r.Get("/heartbeat", noop, SSEStream[struct{}](http.StatusOK, "ping"))
	op := operation(t, r, "/heartbeat", "GET")
	data := op.Responses["200"].
		Content["text/event-stream"].
		ItemSchema.Properties["data"]
	if data.Type != "string" {
		t.Errorf("data type: %q", data.Type)
	}
	if data.ContentMediaType != "" || data.ContentSchema != nil {
		t.Errorf(
			"opaque SSE should not annotate data with content* fields, got %+v",
			data,
		)
	}
}

func TestSpec_MultipartMixedStreamEmitsItemSchemaAndEncoding(t *testing.T) {
	r := router.New()
	r.Get("/frames", noop,
		MultipartMixedStream[Token](
			http.StatusOK,
			"frames",
			WithItemContentType("image/png"),
			WithPrefixParts(
				&Encoding{ContentType: "application/json"},
			),
		),
	)
	op := operation(t, r, "/frames", "GET")

	mt, ok := op.Responses["200"].Content["multipart/mixed"]
	if !ok {
		t.Fatalf("missing multipart/mixed, got %v",
			contentKeys(op.Responses["200"].Content))
	}
	if mt.ItemSchema == nil ||
		mt.ItemSchema.Ref != "#/components/schemas/Token" {
		t.Errorf("itemSchema: %+v", mt.ItemSchema)
	}
	if mt.ItemEncoding == nil || mt.ItemEncoding.ContentType != "image/png" {
		t.Errorf("itemEncoding: %+v", mt.ItemEncoding)
	}
	if len(mt.PrefixEncoding) != 1 ||
		mt.PrefixEncoding[0].ContentType != "application/json" {
		t.Errorf("prefixEncoding: %+v", mt.PrefixEncoding)
	}
}

func TestSpec_MultipartMixedStreamDefaultEncoding(t *testing.T) {
	r := router.New()
	r.Get("/frames", noop,
		MultipartMixedStream[Token](http.StatusOK, "frames"),
	)
	op := operation(t, r, "/frames", "GET")
	mt := op.Responses["200"].Content["multipart/mixed"]
	if mt.ItemEncoding == nil ||
		mt.ItemEncoding.ContentType != "application/octet-stream" {
		t.Errorf("default itemEncoding: %+v", mt.ItemEncoding)
	}
	if len(mt.PrefixEncoding) != 0 {
		t.Errorf("expected no prefixEncoding by default, got %+v",
			mt.PrefixEncoding)
	}
}

type IngestParams struct {
	Logs iter.Seq2[LogEntry, error] `body:"" contentType:"application/jsonl, application/x-ndjson"`
}

func TestSpec_IterSeq2RequestBodyEmitsItemSchema(t *testing.T) {
	r := router.New()
	r.Post("/ingest", noopP[IngestParams])
	op := operation(t, r, "/ingest", "POST")
	body := op.RequestBody
	if body == nil {
		t.Fatal("missing requestBody")
	}
	keys := contentKeys(body.Content)
	sort.Strings(keys)
	want := []string{"application/jsonl", "application/x-ndjson"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("content keys: want %v, got %v", want, keys)
	}
	for _, k := range keys {
		mt := body.Content[k]
		if mt.Schema != nil {
			t.Errorf("%s: schema should be empty, got %+v", k, mt.Schema)
		}
		if mt.ItemSchema == nil ||
			mt.ItemSchema.Ref != "#/components/schemas/LogEntry" {
			t.Errorf("%s itemSchema: want $ref to LogEntry, got %+v",
				k, mt.ItemSchema)
		}
	}
}

type SSEIngestParams struct {
	Events iter.Seq2[router.SSEEvent, error] `body:"" contentType:"text/event-stream"`
}

func TestSpec_IterSeq2SSERequestBodyEmitsEventSchema(t *testing.T) {
	r := router.New()
	r.Post("/sse", noopP[SSEIngestParams])
	op := operation(t, r, "/sse", "POST")
	mt, ok := op.RequestBody.Content["text/event-stream"]
	if !ok {
		t.Fatalf("missing text/event-stream, got %v",
			contentKeys(op.RequestBody.Content))
	}
	if mt.ItemSchema == nil || mt.ItemSchema.Type != "object" {
		t.Fatalf("expected SSE event itemSchema, got %+v", mt.ItemSchema)
	}
	if _, ok := mt.ItemSchema.Properties["data"]; !ok {
		t.Errorf("missing data property: %+v", mt.ItemSchema.Properties)
	}
}

type MultipartIngestParams struct {
	Parts iter.Seq2[router.Part, error] `body:"" contentType:"multipart/mixed"`
}

func TestSpec_IterSeq2MultipartRequestBody(t *testing.T) {
	r := router.New()
	r.Post("/mix", noopP[MultipartIngestParams])
	op := operation(t, r, "/mix", "POST")
	mt, ok := op.RequestBody.Content["multipart/mixed"]
	if !ok {
		t.Fatalf("missing multipart/mixed, got %v",
			contentKeys(op.RequestBody.Content))
	}
	if mt.Schema != nil || mt.ItemSchema != nil {
		t.Errorf("multipart input should emit empty media type, got %+v", mt)
	}
}
