// Package todoapi is the server half of the clientgen example. It lives in
// its own package (rather than in main) so the server, the generator
// program, and the round-trip test can all build the same router from one
// definition — which is the whole point: one source of truth.
package todoapi

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// Todo is a single task. Every field is always present on the wire, so
// every field lands in the schema's required set and the generated client
// receives plain value types.
type Todo struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateTodoCommand is the body of a create request.
type CreateTodoCommand struct {
	Title string `json:"title" desc:"Human-readable task title."`
}

// UpdateTodoCommand is the body of a partial update. The pointers are
// load-bearing: they are what lets a caller say "mark this done" without
// also blanking the title, and they survive into the generated client
// because the spec marks these properties optional.
type UpdateTodoCommand struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
}

// TodoEvent is one frame of the change stream.
type TodoEvent struct {
	Kind string `json:"kind"`
	Todo Todo   `json:"todo"`
}

type ListTodosParams struct {
	Completed *bool  `query:"completed" desc:"Filter by completion state."`
	Limit     int    `query:"limit"     desc:"Maximum number of todos to return."`
	Trace     string `                  desc:"Optional correlation id."           header:"X-Trace-Id"`
}

type GetTodoParams struct {
	ID int `path:"id"`
}

type CreateTodoParams struct {
	Body CreateTodoCommand `body:""`
}

type UpdateTodoParams struct {
	ID   int               `path:"id"`
	Body UpdateTodoCommand `          body:""`
}

type StreamTodosParams struct {
	Limit int `query:"limit" desc:"Number of events to emit before closing."`
}

// ErrNotFound is returned by Store when a todo does not exist.
var ErrNotFound = errors.New("todo not found")

// Store is a tiny in-memory repository.
type Store struct {
	mu    sync.Mutex
	next  int
	todos map[int]Todo
	now   func() time.Time
}

// NewStore constructs an empty Store.
func NewStore() *Store {
	return &Store{next: 1, todos: map[int]Todo{}, now: time.Now}
}

func (s *Store) Get(id int) (Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrNotFound
	}
	return t, nil
}

func (s *Store) List(completed *bool, limit int) []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Todo, 0, len(s.todos))
	for id := 1; id < s.next; id++ {
		t, ok := s.todos[id]
		if !ok {
			continue
		}
		if completed != nil && t.Completed != *completed {
			continue
		}
		out = append(out, t)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) Create(cmd CreateTodoCommand) Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Todo{ID: s.next, Title: cmd.Title, CreatedAt: s.now()}
	s.todos[s.next] = t
	s.next++
	return t
}

func (s *Store) Update(id int, cmd UpdateTodoCommand) (Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrNotFound
	}
	if cmd.Title != nil {
		t.Title = *cmd.Title
	}
	if cmd.Completed != nil {
		t.Completed = *cmd.Completed
	}
	s.todos[id] = t
	return t, nil
}

func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.todos[id]; !ok {
		return ErrNotFound
	}
	delete(s.todos, id)
	return nil
}

// API holds the handlers' shared dependencies.
type API struct {
	Store *Store
}

// New constructs an API over an empty store.
func New() *API { return &API{Store: NewStore()} }

// Register wires every endpoint onto a router. The openapi options here
// are the single source of truth: they produce both the served spec and
// the generated client.
func (a *API) Register(r *router.Router) {
	todos := r.Group("/todos", openapi.Tags("Todos"))

	todos.Get("", a.List,
		openapi.OperationID("listTodos"),
		openapi.Summary("List todos"),
		openapi.Description(
			"Returns todos in creation order, optionally filtered by "+
				"completion state and capped by limit.",
		),
		openapi.ReturnsBody[[]Todo](http.StatusOK, "Matching todos"),
	)

	todos.Post("", a.Create,
		openapi.OperationID("createTodo"),
		openapi.Summary("Create a todo"),
		openapi.ReturnsBody[Todo](http.StatusCreated, "The created todo"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest, "Malformed body",
		),
	)

	todos.Get("/{id}", a.Get,
		openapi.OperationID("getTodo"),
		openapi.Summary("Get a todo by id"),
		openapi.ReturnsBody[Todo](http.StatusOK, "The todo"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound, "No todo with that id",
		),
	)

	todos.Patch("/{id}", a.Update,
		openapi.OperationID("updateTodo"),
		openapi.Summary("Partially update a todo"),
		openapi.Description(
			"Only the fields present in the body are changed; omitted "+
				"fields keep their current value.",
		),
		openapi.ReturnsBody[Todo](http.StatusOK, "The updated todo"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound, "No todo with that id",
		),
	)

	todos.Delete("/{id}", a.Delete,
		openapi.OperationID("deleteTodo"),
		openapi.Summary("Delete a todo"),
		openapi.Returns(http.StatusNoContent, "Deleted"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound, "No todo with that id",
		),
	)

	r.Get("/todo-events", a.Stream,
		openapi.OperationID("streamTodoEvents"),
		openapi.Summary("Stream todo events"),
		openapi.Tags("Todos"),
		openapi.StreamsBody[TodoEvent](
			http.StatusOK, "A newline-delimited event stream",
			"application/jsonl",
		),
	)
}

func (a *API) List(c *router.Context, p ListTodosParams) {
	c.JSON(http.StatusOK, a.Store.List(p.Completed, p.Limit))
}

func (a *API) Get(c *router.Context, p GetTodoParams) {
	t, err := a.Store.Get(p.ID)
	if errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("no todo with that id"))
		return
	}
	c.JSON(http.StatusOK, t)
}

func (a *API) Create(c *router.Context, p CreateTodoParams) {
	if p.Body.Title == "" {
		c.JSON(http.StatusBadRequest, router.BadRequest("title is required"))
		return
	}
	t := a.Store.Create(p.Body)
	c.Header("Location", "/todos/"+strconv.Itoa(t.ID))
	c.JSON(http.StatusCreated, t)
}

func (a *API) Update(c *router.Context, p UpdateTodoParams) {
	t, err := a.Store.Update(p.ID, p.Body)
	if errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("no todo with that id"))
		return
	}
	c.JSON(http.StatusOK, t)
}

func (a *API) Delete(c *router.Context, p GetTodoParams) {
	if err := a.Store.Delete(p.ID); errors.Is(err, ErrNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("no todo with that id"))
		return
	}
	c.NoContent()
}

func (a *API) Stream(c *router.Context, p StreamTodosParams) {
	w := c.Stream(http.StatusOK, "application/jsonl")
	defer func() { _ = w.Close() }()
	for _, t := range a.Store.List(nil, p.Limit) {
		if err := w.Send(TodoEvent{Kind: "snapshot", Todo: t}); err != nil {
			return
		}
	}
}

// Info is the document metadata shared by the served spec and the
// generated client.
func Info() openapi.Info {
	return openapi.Info{
		Title:       "Todo API",
		Version:     "1.0.0",
		Description: "The example API the generated client is built from.",
	}
}

// NewRouter builds a router with every endpoint registered.
func NewRouter() (*router.Router, *API) {
	api := New()
	r := router.New()
	api.Register(r)
	return r, api
}

// Spec derives the OpenAPI document for a router.
func Spec(r *router.Router) *openapi.Document {
	return openapi.NewGenerator(Info()).Spec(r)
}

// SpecHandler serves the OpenAPI document as JSON.
func SpecHandler(r *router.Router) http.HandlerFunc {
	return openapi.NewGenerator(Info()).Handler(r)
}
