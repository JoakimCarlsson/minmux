package runtime

import _ "embed"

// Source is the verbatim text of runtime.go, embedded so the generator can
// re-emit it into the generated package with its package clause rewritten.
//
// Keeping the support code in a real, compiled package rather than an
// inert template string means everything that ships inside a generated
// client is type-checked, vetted, and unit-tested in CI.
//
//go:embed runtime.go
var Source string
