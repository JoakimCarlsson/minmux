module github.com/joakimcarlsson/minmux/examples/todo

go 1.25

require (
	github.com/joakimcarlsson/minmux/openapi v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
)

replace (
	github.com/joakimcarlsson/minmux/openapi => ../../openapi
	github.com/joakimcarlsson/minmux/router => ../../router
)
