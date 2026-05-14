package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
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
		return Todo{}, router.NotFound("todo not found")
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
		return Todo{}, router.NotFound("todo not found")
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
		return router.NotFound("todo not found")
	}
	delete(s.todos, id)
	return nil
}

// API hangs handlers off a struct holding their shared dependencies.
type API struct {
	store *Store
}

func (a *API) Register(r *router.Router) {
	todos := r.Group("/api/v1/todos").Tags("Todos")

	todos.Get("", a.List).
		Summary("List todos").
		Description("Return all todos. Filter by ?completed=true|false, bound by ?limit=N.")

	todos.Get("/{id}", a.Get).
		Summary("Get a todo").
		Description("Returns 404 if the todo does not exist.")

	todos.Post("", a.Create).
		Summary("Create a todo")

	todos.Patch("/{id}", a.Update).
		Summary("Update a todo").
		Description("Partial update. Pass only the fields you want to change.")

	todos.Delete("/{id}", a.Delete).
		Summary("Delete a todo")
}

func (a *API) List(_ context.Context, p ListTodosParams) ([]Todo, error) {
	return a.store.List(p.Completed, p.Limit), nil
}

func (a *API) Get(_ context.Context, p GetTodoParams) (Todo, error) {
	return a.store.Get(p.ID)
}

func (a *API) Create(
	_ context.Context,
	p CreateTodoParams,
) (router.Created[Todo], error) {
	t := a.store.Create(p.Body)
	return router.Created[Todo]{
		Value:    t,
		Location: "/api/v1/todos/" + strconv.Itoa(t.ID),
	}, nil
}

func (a *API) Update(_ context.Context, p UpdateTodoParams) (Todo, error) {
	return a.store.Update(p.ID, p.Body)
}

func (a *API) Delete(
	_ context.Context,
	p DeleteTodoParams,
) (router.NoContent, error) {
	return router.NoContent{}, a.store.Delete(p.ID)
}

func main() {
	api := &API{store: NewStore()}
	r := router.New()
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
