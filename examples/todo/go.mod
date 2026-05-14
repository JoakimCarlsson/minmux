module github.com/joakimcarlsson/minmux/examples/todo

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/cors v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/openapi v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/outputcache v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/outputcache/inmemory v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
)

replace (
	github.com/joakimcarlsson/minmux/cors => ../../cors
	github.com/joakimcarlsson/minmux/openapi => ../../openapi
	github.com/joakimcarlsson/minmux/outputcache => ../../outputcache
	github.com/joakimcarlsson/minmux/outputcache/inmemory => ../../outputcache/inmemory
	github.com/joakimcarlsson/minmux/router => ../../router
)
