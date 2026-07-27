package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/joakimcarlsson/minmux/clientgen"
	todoclient "github.com/joakimcarlsson/minmux/examples/clientgen/client"
	"github.com/joakimcarlsson/minmux/examples/clientgen/todoapi"
)

func newClient(t *testing.T) *todoclient.Client {
	t.Helper()
	r, _ := todoapi.NewRouter()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	c, err := todoclient.New(srv.URL)
	if err != nil {
		t.Fatalf("constructing client: %v", err)
	}
	return c
}

// TestRoundTrip_CreateAndGet is the core acceptance case: a generated
// method reaches the right method and path, encodes its typed params, and
// decodes the response into the typed result.
func TestRoundTrip_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	created, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
		Body: todoclient.CreateTodoCommand{Title: "write the client"},
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if created.ID == 0 {
		t.Errorf("expected a server-assigned id, got %d", created.ID)
	}
	if created.Title != "write the client" {
		t.Errorf("title: got %q", created.Title)
	}
	if created.Completed {
		t.Errorf("a new todo should not be completed")
	}
	if created.CreatedAt.IsZero() {
		t.Errorf("created_at should decode into a real time.Time")
	}

	got, err := c.GetTodo(ctx, todoclient.GetTodoParams{ID: created.ID})
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if got != created {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, created)
	}
}

// TestRoundTrip_PartialUpdatePreservesOmittedFields is the test that
// justifies emitting `required` from the openapi package. UpdateTodoCommand
// carries pointers precisely so that omitting Title leaves the stored title
// alone; if the generated type collapsed those to value types, this would
// blank the title instead.
func TestRoundTrip_PartialUpdatePreservesOmittedFields(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	created, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
		Body: todoclient.CreateTodoCommand{Title: "keep me"},
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}

	done := true
	updated, err := c.UpdateTodo(ctx, todoclient.UpdateTodoParams{
		ID:   created.ID,
		Body: todoclient.UpdateTodoCommand{Completed: &done},
	})
	if err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}
	if !updated.Completed {
		t.Errorf("completed should have been set")
	}
	if updated.Title != "keep me" {
		t.Errorf("title should survive a partial update, got %q", updated.Title)
	}

	title := "renamed"
	renamed, err := c.UpdateTodo(ctx, todoclient.UpdateTodoParams{
		ID:   created.ID,
		Body: todoclient.UpdateTodoCommand{Title: &title},
	})
	if err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}
	if renamed.Title != "renamed" {
		t.Errorf("title: got %q", renamed.Title)
	}
	if !renamed.Completed {
		t.Errorf("completed should survive a title-only update")
	}
}

// TestRoundTrip_NotFoundIsTypedError checks that a non-2xx surfaces as an
// *APIError carrying the body decoded into the type the spec declares.
func TestRoundTrip_NotFoundIsTypedError(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	_, err := c.GetTodo(ctx, todoclient.GetTodoParams{ID: 404404})
	if err == nil {
		t.Fatal("expected an error for a missing todo")
	}

	var apiErr *todoclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("status: got %d, want 404", apiErr.StatusCode)
	}
	if len(apiErr.Body) == 0 {
		t.Errorf("raw body should always be retained")
	}

	pd, ok := todoclient.ErrorValue[*todoclient.ProblemDetails](err)
	if !ok {
		t.Fatalf(
			"error body should decode into *ProblemDetails, got %v",
			apiErr.Value,
		)
	}
	if pd.Status != 404 {
		t.Errorf("ProblemDetails.Status: got %d, want 404", pd.Status)
	}
	if pd.Detail == nil || *pd.Detail != "no todo with that id" {
		t.Errorf("ProblemDetails.Detail: got %v", pd.Detail)
	}
}

func TestRoundTrip_BadRequestIsTypedError(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	_, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
		Body: todoclient.CreateTodoCommand{Title: ""},
	})
	if err == nil {
		t.Fatal("expected an error for an empty title")
	}
	pd, ok := todoclient.ErrorValue[*todoclient.ProblemDetails](err)
	if !ok {
		t.Fatalf("want a decoded ProblemDetails, got %v", err)
	}
	if pd.Status != 400 {
		t.Errorf("status: got %d, want 400", pd.Status)
	}
}

// TestRoundTrip_QueryAndHeaderParams checks that optional parameters are
// omitted when nil and encoded when set.
func TestRoundTrip_QueryAndHeaderParams(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	for _, title := range []string{"a", "b", "c"} {
		_, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
			Body: todoclient.CreateTodoCommand{Title: title},
		})
		if err != nil {
			t.Fatalf("seeding %q: %v", title, err)
		}
	}

	all, err := c.ListTodos(ctx, todoclient.ListTodosParams{})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 todos, got %d", len(all))
	}

	limit := int64(2)
	trace := "trace-123"
	limited, err := c.ListTodos(ctx, todoclient.ListTodosParams{
		Limit:    &limit,
		XTraceID: &trace,
	})
	if err != nil {
		t.Fatalf("ListTodos with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit=2 should return 2 todos, got %d", len(limited))
	}

	done := true
	_, err = c.UpdateTodo(ctx, todoclient.UpdateTodoParams{
		ID:   all[0].ID,
		Body: todoclient.UpdateTodoCommand{Completed: &done},
	})
	if err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}

	completed, err := c.ListTodos(ctx, todoclient.ListTodosParams{
		Completed: &done,
	})
	if err != nil {
		t.Fatalf("ListTodos completed=true: %v", err)
	}
	if len(completed) != 1 {
		t.Errorf("completed=true should return 1 todo, got %d", len(completed))
	}
}

// TestRoundTrip_NoContentAndRepeatedDelete covers the bodiless-success
// signature and the error path on the same method.
func TestRoundTrip_NoContentAndRepeatedDelete(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	created, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
		Body: todoclient.CreateTodoCommand{Title: "temporary"},
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if err := c.DeleteTodo(
		ctx,
		todoclient.DeleteTodoParams{ID: created.ID},
	); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}

	err = c.DeleteTodo(ctx, todoclient.DeleteTodoParams{ID: created.ID})
	if err == nil {
		t.Fatal("deleting twice should fail")
	}
	var apiErr *todoclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("want a 404 APIError, got %v", err)
	}
}

// TestRoundTrip_StreamingYieldsTypedItems exercises the iter.Seq2 return
// shape end to end against a real sequential-JSON response.
func TestRoundTrip_StreamingYieldsTypedItems(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	for _, title := range []string{"one", "two"} {
		_, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
			Body: todoclient.CreateTodoCommand{Title: title},
		})
		if err != nil {
			t.Fatalf("seeding %q: %v", title, err)
		}
	}

	seq, err := c.StreamTodoEvents(ctx, todoclient.StreamTodoEventsParams{})
	if err != nil {
		t.Fatalf("StreamTodoEvents: %v", err)
	}

	var titles []string
	for ev, err := range seq {
		if err != nil {
			t.Fatalf("stream item: %v", err)
		}
		if ev.Kind != "snapshot" {
			t.Errorf("kind: got %q", ev.Kind)
		}
		titles = append(titles, ev.Todo.Title)
	}
	if len(titles) != 2 || titles[0] != "one" || titles[1] != "two" {
		t.Errorf("streamed titles: got %v, want [one two]", titles)
	}
}

// TestRoundTrip_StreamingBreaksEarly checks that abandoning iteration part
// way through does not deadlock or leak the response body.
func TestRoundTrip_StreamingBreaksEarly(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	for _, title := range []string{"one", "two", "three"} {
		_, err := c.CreateTodo(ctx, todoclient.CreateTodoParams{
			Body: todoclient.CreateTodoCommand{Title: title},
		})
		if err != nil {
			t.Fatalf("seeding %q: %v", title, err)
		}
	}

	seq, err := c.StreamTodoEvents(ctx, todoclient.StreamTodoEventsParams{})
	if err != nil {
		t.Fatalf("StreamTodoEvents: %v", err)
	}
	seen := 0
	for range seq {
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("want exactly one item before breaking, got %d", seen)
	}
}

// TestRoundTrip_TransportIsPluggable verifies the seam the issue calls out
// for auth, retries, and tracing.
func TestRoundTrip_TransportIsPluggable(t *testing.T) {
	ctx := context.Background()
	r, _ := todoapi.NewRouter()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	var sawAuth string
	c, err := todoclient.New(srv.URL,
		todoclient.WithHeader("X-Tenant", "acme"),
		todoclient.WithRequestHook(func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer token")
			sawAuth = req.Header.Get("Authorization")
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("constructing client: %v", err)
	}
	if _, err := c.ListTodos(ctx, todoclient.ListTodosParams{}); err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if sawAuth != "Bearer token" {
		t.Errorf("request hook did not run, got %q", sawAuth)
	}
}

// TestGeneratedClientIsUpToDate is the drift guard: regenerating from the
// current handler signatures must reproduce the committed client exactly.
// It fails the moment a handler changes without `go generate ./...` being
// re-run, which is the whole reason the client is committed.
func TestGeneratedClientIsUpToDate(t *testing.T) {
	r, _ := todoapi.NewRouter()
	files, err := clientgen.Render(clientgen.Config{
		Spec:    todoapi.Spec(r),
		Package: "todoclient",
		Command: "go generate ./...",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, f := range files {
		path := filepath.Join("client", f.Name)
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			continue
		}
		if !bytes.Equal(committed, f.Content) {
			t.Errorf(
				"%s is out of date; run `go generate ./...` in %s",
				path, "examples/clientgen",
			)
		}
	}
}
