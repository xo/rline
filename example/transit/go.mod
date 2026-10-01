module github.com/xo/rline/example/transit

go 1.27.1

require (
	github.com/xo/rline v0.0.0-00010101000000-000000000000
	github.com/xo/transit v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/bash v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/c v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/go v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/javascript v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/json v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/python v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/ruby v0.0.0-00010101000000-000000000000
	github.com/xo/transit/grammars/rust v0.0.0-00010101000000-000000000000
)

require (
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// transit has no tag yet, so every module here comes from the checkouts
// beside this one: rline two folders up, and transit beside rline.
replace (
	github.com/xo/rline => ../..
	github.com/xo/transit => ../../../transit
	github.com/xo/transit/grammars/bash => ../../../transit/grammars/bash
	github.com/xo/transit/grammars/c => ../../../transit/grammars/c
	github.com/xo/transit/grammars/go => ../../../transit/grammars/go
	github.com/xo/transit/grammars/javascript => ../../../transit/grammars/javascript
	github.com/xo/transit/grammars/json => ../../../transit/grammars/json
	github.com/xo/transit/grammars/python => ../../../transit/grammars/python
	github.com/xo/transit/grammars/ruby => ../../../transit/grammars/ruby
	github.com/xo/transit/grammars/rust => ../../../transit/grammars/rust
)
