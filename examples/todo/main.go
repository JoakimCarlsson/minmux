package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/joakimcarlsson/minmux/cors"
	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/outputcache"
	"github.com/joakimcarlsson/minmux/outputcache/inmemory"
	"github.com/joakimcarlsson/minmux/router"
)

type Todo struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTodoCommand struct {
	Title string `json:"title"`
}

type UpdateTodoCommand struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
}

type GetTodoParams struct {
	ID int `path:"id"`
}

type ListTodosParams struct {
	Completed *bool `query:"completed"`
	Limit     int   `query:"limit"`
}

type CreateTodoParams struct {
	Body CreateTodoCommand `body:""`
}

type UpdateTodoParams struct {
	ID   int               `path:"id"`
	Body UpdateTodoCommand `          body:""`
}

type DeleteTodoParams struct {
	ID int `path:"id"`
}

var ErrTodoNotFound = errors.New("todo not found")

// Store is a tiny in-memory todo repository.
type Store struct {
	mu    sync.Mutex
	next  int
	todos map[int]Todo
	now   func() time.Time
}

func NewStore() *Store {
	return &Store{next: 1, todos: map[int]Todo{}, now: time.Now}
}

func (s *Store) Get(id int) (Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrTodoNotFound
	}
	return t, nil
}

func (s *Store) List(completed *bool, limit int) []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Todo, 0, len(s.todos))
	for _, t := range s.todos {
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
	t := Todo{
		ID:        s.next,
		Title:     cmd.Title,
		CreatedAt: s.now(),
	}
	s.todos[s.next] = t
	s.next++
	return t
}

func (s *Store) Update(id int, cmd UpdateTodoCommand) (Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrTodoNotFound
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
		return ErrTodoNotFound
	}
	delete(s.todos, id)
	return nil
}

// API hangs handlers off a struct holding their shared dependencies.
type API struct {
	store *Store
	cache *outputcache.Cache
}

func (a *API) Register(r *router.Router) {
	todos := r.Group("/api/v1/todos", openapi.Tags("Todos"))

	todos.Get(
		"",
		a.List,
		openapi.Summary("List todos"),
		openapi.Description(
			"Return all todos. Filter by ?completed=true|false, bound by ?limit=N.",
		),
		openapi.ReturnsBody[[]Todo](http.StatusOK, "Todo list"),
		outputcache.WithOutputCache(time.Minute,
			outputcache.VaryByQuery("completed", "limit"),
			outputcache.Tags("todos"),
		),
	)

	todos.Get(
		"/{id}",
		a.Get,
		openapi.Summary("Get a todo"),
		openapi.ReturnsBody[Todo](http.StatusOK, "Todo found"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound,
			"Todo not found",
		),
		outputcache.WithOutputCache(time.Minute,
			outputcache.Tags("todos"),
		),
	)

	todos.Post(
		"",
		a.Create,
		openapi.Summary("Create a todo"),
		openapi.ReturnsBody[Todo](http.StatusCreated, "Todo created"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest,
			"Invalid body",
		),
	)

	todos.Patch(
		"/{id}",
		a.Update,
		openapi.Summary("Update a todo"),
		openapi.Description(
			"Partial update. Pass only the fields you want to change.",
		),
		openapi.ReturnsBody[Todo](http.StatusOK, "Todo updated"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound,
			"Todo not found",
		),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest,
			"Invalid body",
		),
	)

	todos.Delete(
		"/{id}",
		a.Delete,
		openapi.Summary("Delete a todo"),
		openapi.Returns(http.StatusNoContent, "Todo deleted"),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusNotFound,
			"Todo not found",
		),
	)
}

func (a *API) List(c *router.Context, p ListTodosParams) {
	c.JSON(http.StatusOK, a.store.List(p.Completed, p.Limit))
}

func (a *API) Get(c *router.Context, p GetTodoParams) {
	t, err := a.store.Get(p.ID)
	if errors.Is(err, ErrTodoNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("todo not found"))
		return
	}
	c.JSON(http.StatusOK, t)
}

func (a *API) Create(c *router.Context, p CreateTodoParams) {
	t := a.store.Create(p.Body)
	a.cache.InvalidateTag("todos")
	c.Header("Location", "/api/v1/todos/"+strconv.Itoa(t.ID))
	c.JSON(http.StatusCreated, t)
}

func (a *API) Update(c *router.Context, p UpdateTodoParams) {
	t, err := a.store.Update(p.ID, p.Body)
	if errors.Is(err, ErrTodoNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("todo not found"))
		return
	}
	a.cache.InvalidateTag("todos")
	c.JSON(http.StatusOK, t)
}

func (a *API) Delete(c *router.Context, p DeleteTodoParams) {
	if err := a.store.Delete(p.ID); errors.Is(err, ErrTodoNotFound) {
		c.JSON(http.StatusNotFound, router.NotFound("todo not found"))
		return
	}
	a.cache.InvalidateTag("todos")
	c.NoContent()
}

func main() {
	api := &API{store: NewStore()}
	r := router.New()
	r.Use(cors.Default())

	cache := outputcache.New(r, outputcache.Config{
		Storage:         inmemory.New(),
		DefaultDuration: time.Minute,
	})
	api.cache = cache
	r.Use(cache.Middleware())

	api.Register(r)

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Todo API",
		Version:     "0.1.0",
		Description: "Example API exercising minmux features.",
	})
	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
