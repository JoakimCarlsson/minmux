module github.com/joakimcarlsson/minmux/auth

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/openapi v0.2.0
	github.com/joakimcarlsson/minmux/router v0.1.0
)

replace github.com/joakimcarlsson/minmux/router => ../router

replace github.com/joakimcarlsson/minmux/openapi => ../openapi
