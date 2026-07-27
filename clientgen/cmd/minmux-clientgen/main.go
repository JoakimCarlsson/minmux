// Command minmux-clientgen generates a typed Go client from an OpenAPI
// document on disk.
//
// It is a thin wrapper over clientgen.Generate for the case where the spec
// is already a file. When the server is a Go program you can import,
// prefer calling clientgen.Generate directly from a go:generate program:
// that path hands the generator the in-memory document and skips the JSON
// round trip entirely.
//
//	minmux-clientgen -spec openapi.json -package todoclient -out ./client
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/joakimcarlsson/minmux/clientgen"
	"github.com/joakimcarlsson/minmux/openapi"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "minmux-clientgen: %v\n", err)
		os.Exit(1)
	}
}

// run is the whole program, parameterized over its inputs so it can be
// driven from a test without touching process globals.
func run(args []string, stdin io.Reader, stderr io.Writer) error {
	fs := flag.NewFlagSet("minmux-clientgen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	specPath := fs.String(
		"spec", "", "path to the OpenAPI document (JSON), or - for stdin",
	)
	pkg := fs.String("package", "", "package name of the generated client")
	out := fs.String("out", "", "directory to write the generated client to")
	clientName := fs.String(
		"client", "Client", "name of the generated client type",
	)
	command := fs.String(
		"command", "", "value recorded in the generated-by header",
	)

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || *pkg == "" || *out == "" {
		fs.Usage()
		return fmt.Errorf("-spec, -package, and -out are all required")
	}

	doc, err := loadSpec(*specPath, stdin)
	if err != nil {
		return err
	}
	label := *command
	if label == "" {
		label = fmt.Sprintf(
			"minmux-clientgen -spec %s -package %s -out %s",
			*specPath, *pkg, *out,
		)
	}
	return clientgen.Generate(clientgen.Config{
		Spec:       doc,
		Package:    *pkg,
		Out:        *out,
		ClientName: *clientName,
		Command:    label,
	})
}

func loadSpec(path string, stdin io.Reader) (*openapi.Document, error) {
	data, err := readSpec(path, stdin)
	if err != nil {
		return nil, err
	}
	var doc openapi.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(doc.Paths) == 0 {
		return nil, fmt.Errorf("%s: document declares no paths", path)
	}
	return &doc, nil
}

func readSpec(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}
