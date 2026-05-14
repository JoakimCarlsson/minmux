// security is a runnable smoke test for the openapi package's
// Security Scheme support. Each endpoint demonstrates a different
// OAS 3.2.0 security mechanism, and the generator emits the full
// matrix in /openapi.json including the new deviceAuthorization
// OAuth2 flow.
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

type Profile struct {
	ID   string `json:"id"   format:"uuid"`
	Name string `json:"name"`
}

type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type DeletePetParams struct {
	ID int `path:"id"`
}

func me(c *router.Context) {
	c.JSON(
		http.StatusOK,
		Profile{ID: "00000000-0000-0000-0000-000000000000", Name: "Joe"},
	)
}

func health(c *router.Context) {
	c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func listPets(c *router.Context) {
	c.JSON(http.StatusOK, []Pet{{ID: 1, Name: "Fido"}})
}

func createPet(c *router.Context) {
	c.JSON(http.StatusCreated, Pet{ID: 2, Name: "Rex"})
}

func deletePet(c *router.Context, _ DeletePetParams) {
	c.NoContent()
}

func deviceLogin(c *router.Context) {
	c.JSON(http.StatusOK, map[string]string{"flow": "device"})
}

func main() {
	r := router.New()

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Security Showcase",
		Version:     "0.1.0",
		Description: "Smoke test for OAS 3.2.0 securitySchemes (incl. deviceAuthorization).",
	})

	// All five OAS 3.2 scheme types, plus the new deviceAuthorization
	// OAuth2 flow.
	gen.SecuritySchemes = map[string]*openapi.SecurityScheme{
		"basicAuth":  openapi.BasicAuth("Username + password over HTTPS"),
		"bearerAuth": openapi.BearerAuth("JWT", "Bearer JWT issued by the IdP"),
		"apiKeyAuth": openapi.APIKey(
			"header",
			"X-Api-Key",
			"Long-lived service key",
		),
		"mtls": openapi.MutualTLS(
			"Client cert must chain to example.com CA",
		),
		"oidc": openapi.OpenIDConnect(
			"https://issuer.example/.well-known/openid-configuration",
			"",
		),
		"petstoreOAuth": openapi.OAuth2Scheme(&openapi.OAuthFlows{
			AuthorizationCode: &openapi.OAuthFlow{
				AuthorizationURL: "https://auth.example/oauth/authorize",
				TokenURL:         "https://auth.example/oauth/token",
				RefreshURL:       "https://auth.example/oauth/refresh",
				Scopes: map[string]string{
					"read:pets":  "List and read pets",
					"write:pets": "Create, update, and delete pets",
				},
			},
			ClientCredentials: &openapi.OAuthFlow{
				TokenURL: "https://auth.example/oauth/token",
				Scopes: map[string]string{
					"admin:pets": "Administer pets (service-to-service)",
				},
			},
			// OAS 3.2 introduces the deviceAuthorization flow (RFC 8628).
			DeviceAuthorization: &openapi.OAuthFlow{
				DeviceAuthorizationURL: "https://auth.example/oauth/device_authorization",
				TokenURL:               "https://auth.example/oauth/token",
				Scopes: map[string]string{
					"read:pets": "List and read pets (from a TV or CLI)",
				},
			},
		}, "Pet store user OAuth2 grants"),
	}
	// oauth2MetadataUrl is also new in 3.2; set it on the OAuth2
	// scheme so tooling can discover the AS configuration.
	gen.SecuritySchemes["petstoreOAuth"].OAuth2MetadataURL =
		"https://auth.example/.well-known/oauth-authorization-server"

	// Document-level default: every operation requires bearerAuth
	// unless it overrides via NoSecurity or its own Security calls.
	gen.Security = []openapi.SecurityRequirement{
		{"bearerAuth": {}},
	}

	r.Get("/health", health,
		openapi.Summary("Liveness probe"),
		openapi.Tags("Meta"),
		openapi.ReturnsBody[map[string]string](http.StatusOK, "Healthy"),
		openapi.NoSecurity(),
	)

	r.Get("/me", me,
		openapi.Summary("Current user profile"),
		openapi.Tags("Users"),
		openapi.ReturnsBody[Profile](http.StatusOK, "Profile"),
		// inherits the document-level bearerAuth default
	)

	pets := r.Group("/pets",
		openapi.Tags("Pets"),
		openapi.Security("petstoreOAuth", "read:pets"),
	)

	pets.Get("", listPets,
		openapi.Summary("List pets"),
		openapi.ReturnsBody[[]Pet](http.StatusOK, "Pets"),
		// Allow anonymous as an alternative to the group default.
		openapi.OptionalSecurity(),
	)

	pets.Post("", createPet,
		openapi.Summary("Create a pet"),
		openapi.ReturnsBody[Pet](http.StatusCreated, "Pet created"),
		// Override the group's read:pets requirement with write:pets.
		openapi.Security("petstoreOAuth", "write:pets"),
	)

	pets.Delete("/{id}", deletePet,
		openapi.Summary("Delete a pet (admin only)"),
		openapi.Returns(http.StatusNoContent, "Deleted"),
		// AND: must satisfy both mTLS and the oauth2 admin scope.
		openapi.SecurityAll(openapi.SecurityRequirement{
			"mtls":          {},
			"petstoreOAuth": {"admin:pets"},
		}),
	)

	r.Post("/device/login", deviceLogin,
		openapi.Summary("Begin a device authorization flow"),
		openapi.Tags("Auth"),
		openapi.ReturnsBody[map[string]string](http.StatusOK, "Flow started"),
		// Demonstrates the OAS 3.2 deviceAuthorization flow as an
		// alternative to the document-level bearer requirement.
		openapi.Security("petstoreOAuth", "read:pets"),
		openapi.Security("apiKeyAuth"),
	)

	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	addr := ":8080"
	fmt.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
