module github.com/joakimcarlsson/minmux/examples/auth

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/auth v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/openapi v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
)

replace (
	github.com/joakimcarlsson/minmux/auth => ../../auth
	github.com/joakimcarlsson/minmux/openapi => ../../openapi
	github.com/joakimcarlsson/minmux/router => ../../router
)
