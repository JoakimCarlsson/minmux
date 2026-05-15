package main

import (
	"log"
	"net/http"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

type Widget struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	r := router.New()

	r.Options("/widgets", func(c *router.Context) {
		c.Header("Allow", "GET, POST, OPTIONS, HEAD")
		c.NoContent()
	},
		openapi.Summary("Advertise supported methods on /widgets"),
		openapi.Tags("Widgets"),
	)

	r.Head(
		"/widgets/{id}",
		func(c *router.Context) {
			c.Header("X-Widget-Count", "1")
			c.NoContent()
		},
		openapi.Summary(
			"Probe whether a widget exists without fetching its body",
		),
		openapi.Tags("Widgets"),
	)

	r.Get("/widgets/{id}", func(c *router.Context) {
		c.JSON(http.StatusOK, Widget{ID: 1, Name: "gear"})
	},
		openapi.Summary("Fetch a widget"),
		openapi.Tags("Widgets"),
		openapi.ReturnsBody[Widget](http.StatusOK, "The widget"),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:   "Verbs example",
		Version: "0.1.0",
	})

	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
