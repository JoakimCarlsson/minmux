// Package openapi generates an OpenAPI 3.x specification from endpoints
// registered on a minmux router.
//
// Unlike approaches that require restating request/response types in
// documentation options, openapi derives schemas directly from the
// handler signature: parameter struct fields become path/query/header/body
// schemas, and typed Results become response schemas.
package openapi
