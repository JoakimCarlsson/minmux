// Command clientgen-example serves the Todo API that the committed client
// in ./client is generated from.
//
// Run `go generate ./...` to regenerate the client after changing a
// handler signature; see ./gen for the generator program and
// roundtrip_test.go for the client exercised against the real server.
package main

//go:generate go run ./gen

import (
	"log"
	"net/http"

	"github.com/joakimcarlsson/minmux/examples/clientgen/todoapi"
)

func main() {
	r, _ := todoapi.NewRouter()

	r.HandleFunc(http.MethodGet, "/openapi.json", todoapi.SpecHandler(r))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
