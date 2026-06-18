package main

import (
	"log"
	"net/http"
	"time"

	"github.com/joakimcarlsson/minmux/openapi"
	"github.com/joakimcarlsson/minmux/router"
)

// Numbers exercises every Go numeric kind so the generated schema can
// be checked against the OAS 3.2 format mapping.
type Numbers struct {
	I8  int8  `json:"i8"`
	I16 int16 `json:"i16"`
	I32 int32 `json:"i32"`
	I64 int64 `json:"i64"`
	I   int   `json:"i"`

	U8  uint8  `json:"u8"`
	U16 uint16 `json:"u16"`
	U32 uint32 `json:"u32"`
	U64 uint64 `json:"u64"`
	U   uint   `json:"u"`

	F32 float32 `json:"f32"`
	F64 float64 `json:"f64"`
}

// Strings exercises the format:"..." struct tag with a representative
// spread of OAS-registered string formats. The OAS-defined formats
// (`password`) and registered formats (`email`, `uuid`, `uri`, `date`,
// `time`, `ipv4`, `ipv6`, `hostname`, `regex`, `byte`, `binary`) are
// passed through opaquely.
type Strings struct {
	Plain    string `json:"plain"`
	Email    string `json:"email"    format:"email"`
	Password string `json:"password" format:"password"`
	UUID     string `json:"uuid"     format:"uuid"`
	URI      string `json:"uri"      format:"uri"`
	Date     string `json:"date"     format:"date"`
	Time     string `json:"time"     format:"time"`
	IPv4     string `json:"ipv4"     format:"ipv4"`
	IPv6     string `json:"ipv6"     format:"ipv6"`
	Hostname string `json:"hostname" format:"hostname"`
	Regex    string `json:"regex"    format:"regex"`
	ByteB64  string `json:"byte_b64" format:"byte"`
	Binary   string `json:"binary"   format:"binary"`
}

// Showcase is the canonical response: every numeric variant, every
// format-tagged string variant, plus time.Time (auto date-time) and
// nested slice/map/struct cases for sanity.
type Showcase struct {
	Numbers Numbers   `json:"numbers"`
	Strings Strings   `json:"strings"`
	When    time.Time `json:"when"`
	Bool    bool      `json:"bool"`

	IntList    []int             `json:"int_list"`
	StringMap  map[string]string `json:"string_map"`
	NestedList []Strings         `json:"nested_list"`
}

// NumberPathParams shows a path int32 alongside a tag-overridden int64
// query so the generator's auto-format and tag-override paths are both
// exercised. Trace/Limit are optional (the default); Key uses the
// ",required" modifier, so the spec marks it required:true (documentation
// only — the binder does not reject a missing query param).
type NumberPathParams struct {
	ID    int32  `path:"id"`
	Trace string `          query:"trace" format:"uuid"`
	Limit int32  `          query:"limit" format:"int64"`
	Key   string `          query:"key,required"`
}

// CreateUserCommand is the request body covering the most common
// format-tagged string fields.
type CreateUserCommand struct {
	Email    string    `json:"email"     format:"email"`
	Password string    `json:"password"  format:"password"`
	Birthday string    `json:"birthday"  format:"date"`
	Avatar   string    `json:"avatar"    format:"uri"`
	JoinedAt time.Time `json:"joined_at"`
}

// CreateUserParams wraps the body for the typed dispatcher.
type CreateUserParams struct {
	Body CreateUserCommand `body:""`
}

func showcase(c *router.Context) {
	c.JSON(http.StatusOK, sample())
}

func numbers(c *router.Context, p NumberPathParams) {
	c.JSON(http.StatusOK, map[string]any{
		"id":    p.ID,
		"trace": p.Trace,
		"limit": p.Limit,
	})
}

func createUser(c *router.Context, p CreateUserParams) {
	c.Header("Location", "/users/1")
	c.JSON(http.StatusCreated, p.Body)
}

func sample() Showcase {
	return Showcase{
		Numbers: Numbers{
			I8: -8, I16: -16, I32: -32, I64: -64, I: -1,
			U8: 8, U16: 16, U32: 32, U64: 64, U: 1,
			F32: 1.5, F64: 2.5,
		},
		Strings: Strings{
			Plain:    "hello",
			Email:    "joe@example.com",
			Password: "hunter2",
			UUID:     "0190d8e0-2c2c-7000-8000-000000000000",
			URI:      "https://example.com",
			Date:     "2026-05-14",
			Time:     "15:30:00",
			IPv4:     "127.0.0.1",
			IPv6:     "::1",
			Hostname: "example.com",
			Regex:    "^foo.*$",
			ByteB64:  "aGVsbG8=",
			Binary:   "binary-bytes",
		},
		When:       time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC),
		Bool:       true,
		IntList:    []int{1, 2, 3},
		StringMap:  map[string]string{"k": "v"},
		NestedList: []Strings{{Plain: "nested"}},
	}
}

func main() {
	r := router.New()

	r.Get(
		"/showcase",
		showcase,
		openapi.Summary("Showcase response"),
		openapi.Description(
			"Returns a value covering every Go primitive and every "+
				"format-tagged string variant so the generated schema "+
				"can be inspected.",
		),
		openapi.Tags("Showcase"),
		openapi.ReturnsBody[Showcase](http.StatusOK, "Showcase payload"),
	)

	r.Get(
		"/showcase/numbers/{id}",
		numbers,
		openapi.Summary("Numeric parameters"),
		openapi.Description(
			"Path int32 + query uuid + query int32-tagged-as-int64 "+
				"to exercise auto formats and the tag-override path.",
		),
		openapi.Tags("Showcase"),
		openapi.ReturnsBody[map[string]any](
			http.StatusOK, "Echo of parsed params",
		),
	)

	r.Post(
		"/showcase/users",
		createUser,
		openapi.Summary("Create user with format-tagged fields"),
		openapi.Tags("Showcase"),
		openapi.ReturnsBody[CreateUserCommand](
			http.StatusCreated, "User created",
		),
		openapi.ReturnsBody[router.ProblemDetails](
			http.StatusBadRequest, "Invalid body",
		),
	)

	gen := openapi.NewGenerator(openapi.Info{
		Title:       "Types Showcase",
		Version:     "0.1.0",
		Description: "Smoke test for openapi schema generation.",
	})
	r.HandleFunc(http.MethodGet, "/openapi.json", gen.Handler(r))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
