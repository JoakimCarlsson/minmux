// Command gen regenerates the typed client in ../client from the same
// router the server runs.
//
// It is invoked by the //go:generate directive in generate.go, so
// `go generate ./...` from the example root rewrites the committed client.
// Because the output is deterministic, a CI step of
// `go generate ./... && git diff --exit-code` fails exactly when the
// committed client has drifted from the handler signatures.
package main

import (
	"log"

	"github.com/joakimcarlsson/minmux/clientgen"
	"github.com/joakimcarlsson/minmux/examples/clientgen/todoapi"
)

func main() {
	r, _ := todoapi.NewRouter()

	err := clientgen.Generate(clientgen.Config{
		Spec:    todoapi.Spec(r),
		Package: "todoclient",
		Out:     "client",
		Command: "go generate ./...",
	})
	if err != nil {
		log.Fatalf("generating client: %v", err)
	}
	log.Println("wrote client/")
}
