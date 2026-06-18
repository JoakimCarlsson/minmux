module github.com/joakimcarlsson/minmux/examples/types-showcase

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/openapi v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/scalar v0.0.0-00010101000000-000000000000
)

replace (
	github.com/joakimcarlsson/minmux/openapi => ../../openapi
	github.com/joakimcarlsson/minmux/router => ../../router
	github.com/joakimcarlsson/minmux/scalar => ../../scalar
)
