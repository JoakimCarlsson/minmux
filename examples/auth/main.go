// auth is a runnable showcase of the auth package: one Verifier turns the
// openapi.Security annotations already on each route into runtime enforcement.
//
//	go run .            # listen on :8080
//
//	# public route — no credential needed:
//	curl -s localhost:8080/public
//
//	# required route — 401 without a token, 200 with one:
//	curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/me
//	curl -s localhost:8080/me -H 'Authorization: Bearer secret-alice'
//
//	# optional route — anonymous is allowed, a valid token personalizes it,
//	# a present-but-invalid token is still rejected (401):
//	curl -s localhost:8080/maybe
//	curl -s localhost:8080/maybe -H 'Authorization: Bearer secret-alice'
//	curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/maybe -H 'Authorization: Bearer nope'
package main

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/joakimcarlsson/minmux/auth"
	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

const bearerScheme = "bearerAuth"

// verifyBearer is the app's credential validator. A real app parses a JWT or
// calls an IdP here; this demo accepts "Bearer secret-<user>" and resolves it
// to the user name.
func verifyBearer(r *http.Request, _ []string) (any, error) {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return nil, auth.ErrNoCredential
	}
	user, ok := strings.CutPrefix(token, "secret-")
	if !ok {
		return nil, errors.New("invalid token")
	}
	return user, nil
}

func public(c *router.Context) {
	c.JSON(http.StatusOK, map[string]string{"open": "to everyone"})
}

func me(c *router.Context) {
	user, _ := auth.Principal[string](c.Ctx())
	c.JSON(http.StatusOK, map[string]string{"user": user})
}

func maybe(c *router.Context) {
	user, ok := auth.Principal[string](c.Ctx())
	if !ok {
		user = "anonymous"
	}
	c.JSON(http.StatusOK, map[string]string{"user": user})
}

func main() {
	r := router.New()

	gen := openapi.NewGenerator(
		openapi.Info{Title: "auth example", Version: "0.1.0"},
	)
	gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
		bearerScheme: openapi.BearerAuth(
			"",
			"Send: Authorization: Bearer secret-<user>",
		),
	}

	authn := auth.New(r, auth.Config{
		Verifiers: map[string]auth.Verifier{bearerScheme: verifyBearer},
	})
	r.Use(authn.Middleware())

	r.Get("/public", public, openapi.NoSecurity())
	r.Get("/me", me, openapi.Security(bearerScheme))
	r.Get("/maybe", maybe,
		openapi.OptionalSecurity(),
		openapi.Security(bearerScheme),
	)

	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
