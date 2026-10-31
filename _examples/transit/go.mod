module github.com/xo/rline/_examples/transit

go 1.27.1

require (
	github.com/xo/rline v0.1.0
	github.com/xo/transit v0.5.0
	github.com/xo/transit/grammars/bash v0.5.0
	github.com/xo/transit/grammars/c v0.5.0
	github.com/xo/transit/grammars/go v0.5.0
	github.com/xo/transit/grammars/javascript v0.5.0
	github.com/xo/transit/grammars/json v0.5.0
	github.com/xo/transit/grammars/python v0.5.0
	github.com/xo/transit/grammars/ruby v0.5.0
	github.com/xo/transit/grammars/rust v0.5.0
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/xo/inputrc v0.8.0 // indirect
	github.com/xo/terminfo v1.3.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace github.com/xo/rline => ../..
