package clientgen

import (
	"io"
	"iter"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

type Todo struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
	Tags      []string  `json:"tags,omitempty"`
}

type CreateTodoCommand struct {
	Title string `json:"title"`
}

type UpdateTodoCommand struct {
	Title     *string `json:"title,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
}

type ListTodosParams struct {
	Completed *bool  `query:"completed"`
	Limit     int    `query:"limit,required"`
	Trace     string `header:"X-Trace-Id"`
}

type GetTodoParams struct {
	ID int `path:"id"`
}

type CreateTodoParams struct {
	Body CreateTodoCommand `body:""`
}

type UpdateTodoParams struct {
	ID   int               `path:"id"`
	Body UpdateTodoCommand `body:""`
}

type UploadParams struct {
	Note string                  `form:"note"`
	File *multipart.FileHeader   `file:"file"`
	Alts []*multipart.FileHeader `file:"alts"`
}

type DownloadParams struct {
	Path string `path:"path"`
}

type Progress struct {
	Percent int    `json:"percent"`
	Stage   string `json:"stage"`
}

func noop(_ *router.Context)                           {}
func listTodos(_ *router.Context, _ ListTodosParams)   {}
func getTodo(_ *router.Context, _ GetTodoParams)       {}
func createTodo(_ *router.Context, _ CreateTodoParams) {}
func updateTodo(_ *router.Context, _ UpdateTodoParams) {}
func deleteTodo(_ *router.Context, _ GetTodoParams)    {}
func upload(_ *router.Context, _ UploadParams)         {}
func download(_ *router.Context, _ DownloadParams)     {}

// fixtureRouter builds a router exercising the shapes the generator has to
// cover: every parameter location, a JSON body, a partial-update body of
// pointers, a multipart upload, a wildcard path, and each streaming
// response kind.
func fixtureRouter() *router.Router {
	r := router.New()
	todos := r.Group("/api/v1/todos", openapi.Tags("Todos"))

	todos.Get("", listTodos,
		openapi.Summary("List todos"),
		openapi.ReturnsBody[[]Todo](http.StatusOK, "Todo list"),
	)
	todos.Get("/{id}", getTodo,
		openapi.Summary("Get a todo"),
		openapi.ReturnsBody[Todo](http.StatusOK, "Todo found"),
		openapi.ReturnsBody[router.ProblemDetails](http.StatusNotFound, ""),
	)
	todos.Post("", createTodo,
		openapi.Summary("Create a todo"),
		openapi.ReturnsBody[Todo](http.StatusCreated, "Todo created"),
		openapi.ReturnsBody[router.ProblemDetails](http.StatusBadRequest, ""),
	)
	todos.Patch("/{id}", updateTodo,
		openapi.Summary("Update a todo"),
		openapi.ReturnsBody[Todo](http.StatusOK, "Todo updated"),
		openapi.ReturnsBody[router.ProblemDetails](http.StatusNotFound, ""),
	)
	todos.Delete("/{id}", deleteTodo,
		openapi.Summary("Delete a todo"),
		openapi.Returns(http.StatusNoContent, "Todo deleted"),
		openapi.ReturnsBody[router.ProblemDetails](http.StatusNotFound, ""),
	)

	r.Post("/uploads", upload,
		openapi.Summary("Upload a file"),
		openapi.ReturnsBody[Todo](http.StatusCreated, "Uploaded"),
	)
	r.Get("/files/{path...}", download,
		openapi.Summary("Download a file"),
		openapi.Returns(http.StatusOK, "File contents"),
	)
	r.Get("/progress", noop,
		openapi.Summary("Watch progress"),
		openapi.StreamsBody[Progress](http.StatusOK, "Progress stream"),
	)
	r.Get("/events", noop,
		openapi.Summary("Subscribe to events"),
		openapi.SSEStream[Progress](http.StatusOK, "Event stream"),
		openapi.ReturnsBody[router.ProblemDetails](http.StatusUnauthorized, ""),
	)
	return r
}

func fixtureSpec() *openapi.Document {
	gen := openapi.NewGenerator(openapi.Info{
		Title:   "Fixture API",
		Version: "1.0.0",
	})
	return gen.Spec(fixtureRouter())
}

var (
	_ io.Reader             = nil
	_ iter.Seq2[int, error] = nil
)
