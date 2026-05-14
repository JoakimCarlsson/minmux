# minmux

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go)](https://go.dev/)

An ASP.NET-Minimal-API-inspired HTTP framework for Go. Handler signatures
drive parameter binding, response serialization, and OpenAPI generation —
no duplicated type declarations. Built on `net/http` ServeMux pattern
routing, so the entire `http.Handler` middleware ecosystem stays
compatible.

## Design

- **Signature is the contract.** A handler like
  `func(ctx, p GetUserParams) (Ok[User], error)` tells the framework how
  to bind the request, how to serialize the response, and what OpenAPI
  schema to emit. The same type information is never written twice.
- **Typed Results.** `Ok[T]`, `Created[T]`, `NotFound`, and
  `ProblemDetails` (RFC 7807) are typed return wrappers. An endpoint can
  return multiple result variants; OpenAPI sees every possible response
  code statically.
- **Per-endpoint filters.** Pre / post hooks with typed access to args
  and return value, can short-circuit. Better than `http.Handler`
  middleware for typed concerns.
- **Conventions cascade.** `MapGroup("/api/v1").WithTags("Users").RequireAuth()`
  flows to every child endpoint.
- **Bake at startup.** Reflection runs once per endpoint at registration;
  request handling is a cached closure call.

## Module structure

minmux is published as independent Go modules. You install only what
you need.

- **`router`** — core: routing, parameter binding, filters, typed
  Results, ProblemDetails.
- **`openapi`** — generates an OpenAPI 3.x spec from registered
  endpoints, deriving schemas from handler signatures.

See [Modules](modules.md) for the full list.

## Status

Early development. The API surface is being designed; expect breaking
changes until a v0.1.0 tag.
