module github.com/joakimcarlsson/minmux/tests/cors-browser

go 1.26.0

require (
	github.com/joakimcarlsson/bonk v0.3.0
	github.com/joakimcarlsson/minmux/cors v0.0.0-00010101000000-000000000000
	github.com/joakimcarlsson/minmux/router v0.0.0-00010101000000-000000000000
)

require (
	github.com/coder/websocket v1.8.14 // indirect
	golang.org/x/image v0.37.0 // indirect
)

replace (
	github.com/joakimcarlsson/minmux/cors => ../../cors
	github.com/joakimcarlsson/minmux/router => ../../router
)
