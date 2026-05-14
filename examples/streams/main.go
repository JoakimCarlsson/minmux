package main

import (
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// Token is the JSON payload carried in each SSE event's data field. The
// openapi schema generator uses this to annotate data with
// contentMediaType: application/json + contentSchema: Token.
type Token struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// LogEntry is the per-record shape for the JSONL log stream and the
// JSONL/JSON-seq ingest endpoint.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// FrameMeta is metadata that accompanies each binary frame in the
// multipart/mixed stream. Real bytes are sent in the part body; the
// X-Frame-Index and X-Captured-At headers carry the per-frame metadata.
type FrameMeta struct {
	Index    int       `json:"index"`
	Captured time.Time `json:"captured"`
}

// IngestParams binds a streaming JSON-record request body. The handler
// ranges over Logs to consume records as they arrive on the wire; the
// underlying r.Body is closed when the iterator is exhausted.
type IngestParams struct {
	Logs iter.Seq2[LogEntry, error] `body:"" contentType:"application/jsonl, application/x-ndjson, application/json-seq"`
}

// IngestReport summarises a completed ingest.
type IngestReport struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

func tokens(c *router.Context) {
	sse := c.SSE(http.StatusOK)
	defer sse.Close()
	for i, word := range strings.Fields("hello from minmux streaming v3 2 0") {
		if err := sse.Send(router.SSEEvent{
			ID:    fmt.Sprintf("%d", i),
			Event: "token",
			Data:  Token{Index: i, Text: word},
		}); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = sse.Send(router.SSEEvent{Event: "done", Data: ""})
}

func logsJSONL(c *router.Context) {
	w := c.Stream(http.StatusOK, "application/jsonl")
	defer w.Close()
	for i := 0; i < 5; i++ {
		if err := w.Send(LogEntry{
			Timestamp: time.Now().UTC(),
			Level:     "info",
			Message:   fmt.Sprintf("tick %d", i),
		}); err != nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func ingest(c *router.Context, p IngestParams) {
	var accepted, rejected int
	for entry, err := range p.Logs {
		if err != nil {
			rejected++
			continue
		}
		log.Printf("ingested: %+v", entry)
		accepted++
	}
	c.JSON(http.StatusOK, IngestReport{Accepted: accepted, Rejected: rejected})
}

func frames(c *router.Context) {
	mp := c.MultipartMixed(http.StatusOK)
	defer mp.Close()
	for i := 0; i < 3; i++ {
		if err := mp.Part(
			textproto.MIMEHeader{
				"Content-Type":  {"application/octet-stream"},
				"X-Frame-Index": {fmt.Sprintf("%d", i)},
				"X-Captured-At": {time.Now().UTC().Format(time.RFC3339Nano)},
			},
			io.LimitReader(strings.NewReader(strings.Repeat("X", 64)), 64),
		); err != nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func main() {
	r := router.New()

	r.Get("/tokens", tokens,
		openapi.Summary("Streaming AI tokens via SSE"),
		openapi.Description(
			"Emits one Server-Sent Event per token. The data field of "+
				"each event carries a JSON-encoded Token value.",
		),
		openapi.Tags("Streams"),
		openapi.SSEStream[Token](http.StatusOK, "Token stream"),
	)

	r.Get("/logs.jsonl", logsJSONL,
		openapi.Summary("Streaming log entries as JSON Lines"),
		openapi.Tags("Streams"),
		openapi.StreamsBody[LogEntry](
			http.StatusOK,
			"Newline-delimited log entries",
			"application/jsonl",
			"application/x-ndjson",
		),
	)

	r.Post("/ingest", ingest,
		openapi.Summary("Ingest a stream of log records"),
		openapi.Description(
			"Accepts JSONL, NDJSON, or application/json-seq. The handler "+
				"ranges over iter.Seq2 so records are consumed as they "+
				"arrive on the wire.",
		),
		openapi.Tags("Streams"),
		openapi.ReturnsBody[IngestReport](http.StatusOK, "Ingest summary"),
	)

	r.Get("/frames", frames,
		openapi.Summary("Streaming frames via multipart/mixed"),
		openapi.Description(
			"Each frame is one application/octet-stream part with "+
				"X-Frame-Index and X-Captured-At metadata headers. "+
				"Matches OAS 3.2 §4.15.4.8 (Streaming Multipart).",
		),
		openapi.Tags("Streams"),
		openapi.MultipartMixedStream[FrameMeta](
			http.StatusOK,
			"Streaming frames",
			openapi.WithItemContentType("application/octet-stream"),
		),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Streams Showcase",
		Version:     "0.1.0",
		Description: "Smoke test for OAS 3.2 streaming bindings.",
	})
	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	addr := ":8080"
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
