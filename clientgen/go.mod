module github.com/joakimcarlsson/minmux/clientgen

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/openapi v0.1.0
	github.com/joakimcarlsson/minmux/router v0.1.0
)

replace (
	github.com/joakimcarlsson/minmux/openapi => ../openapi
	github.com/joakimcarlsson/minmux/router => ../router
)
