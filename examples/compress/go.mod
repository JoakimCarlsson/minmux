module github.com/joakimcarlsson/minmux/examples/compress

go 1.26.0

require (
	github.com/joakimcarlsson/minmux/compress v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/openapi v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/outputcache v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/outputcache/inmemory v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
)

replace (
	github.com/joakimcarlsson/minmux/compress => ../../compress
	github.com/joakimcarlsson/minmux/openapi => ../../openapi
	github.com/joakimcarlsson/minmux/outputcache => ../../outputcache
	github.com/joakimcarlsson/minmux/outputcache/inmemory => ../../outputcache/inmemory
	github.com/joakimcarlsson/minmux/router => ../../router
)
