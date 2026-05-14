# Installation

minmux requires Go 1.25 or later.

## Core router

```bash
go get github.com/joakimcarlsson/minmux/router
```

This gives you routing, typed parameter binding, response helpers
(`c.JSON`, `c.NoContent`, …), and ProblemDetails. No external
dependencies.

## With OpenAPI generation

```bash
go get github.com/joakimcarlsson/minmux/router
go get github.com/joakimcarlsson/minmux/openapi
```

`openapi` adds the spec generator and the option functions
(`openapi.Summary`, `openapi.Tags`, `openapi.Returns`,
`openapi.ReturnsBody[T]`) you attach to route registrations.

## Status

Early development. Modules are not yet tagged; pin to a commit SHA if
you depend on them today.
