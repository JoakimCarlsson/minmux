// Package clientgen generates a typed Go client from the OpenAPI document
// that minmux's openapi package derives from registered handler signatures.
//
// The server spec and the client come from the same source of truth — the
// handler signatures themselves — so a breaking change to a request or
// response type becomes a compile error in the caller rather than a
// runtime 4xx.
//
// # Usage
//
// Call Generate from a small program wired up with go:generate, handing it
// the document your server already serves:
//
//	//go:generate go run ./gen
//
//	func main() {
//	    r := router.New()
//	    api.Register(r)
//	    gen := openapi.NewGenerator(openapi.Info{Title: "Todo API", Version: "1"})
//
//	    err := clientgen.Generate(clientgen.Config{
//	        Spec:    gen.Spec(r),
//	        Package: "todoclient",
//	        Out:     "../client",
//	    })
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// The cmd/minmux-clientgen binary is a thin wrapper over the same entry
// point for the case where you only have a spec file:
//
//	minmux-clientgen -spec openapi.json -package todoclient -out ./client
//
// # Generated shape
//
// Each operation becomes one method taking a context and a single params
// struct, and returning the operation's success type plus an error:
//
//	func (c *Client) GetTodosById(
//	    ctx context.Context, p GetTodosByIdParams,
//	) (Todo, error)
//
// Every response the operation does not declare as its success status
// surfaces as an *APIError, with the body decoded into whatever type the
// spec declares for that status:
//
//	if pd, ok := todoclient.ErrorValue[*todoclient.ProblemDetails](err); ok {
//	    log.Println(pd.Status, pd.Detail)
//	}
//
// Streaming operations return an iter.Seq2 over decoded items. See
// Config for the knobs, and the examples/clientgen module for a server,
// its committed generated client, and a round-trip test.
//
// # Determinism
//
// Output is byte-stable for a given document: types, operations, fields,
// and imports are all emitted in sorted order, and the result is run
// through go/format. Regenerating an unchanged API produces no diff, so
// `go generate ./... && git diff --exit-code` is a usable CI drift check.
package clientgen
