package router

import (
	"encoding/json"
	"io"
)

// Codec encodes and decodes request and response bodies. The default
// implementation uses encoding/json. Swap via WithCodec to plug in a faster
// or schema-validating codec.
type Codec interface {
	ContentType() string
	Encode(w io.Writer, v any) error
	Decode(r io.Reader, v any) error
}

type jsonCodec struct{}

func (jsonCodec) ContentType() string { return "application/json" }

func (jsonCodec) Encode(
	w io.Writer,
	v any,
) error {
	return json.NewEncoder(w).Encode(v)
}

func (jsonCodec) Decode(
	r io.Reader,
	v any,
) error {
	return json.NewDecoder(r).Decode(v)
}
