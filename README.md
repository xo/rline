# rline

Package `rline` provides a standard API wrapping the "readline" packages
([readline][], [replxx][]), and provides a comparable implementation written in
pure Go.

## Quickstart Example

A [comprehensive example][example] is available:

```sh
# get source and build
$ git clone https://github.com/xo/rline.git && cd rline

# build dependencies
$ ./deps.sh

# build and run the example with readline and replxx support
$ go build -o example -tags 'rline_readline rline_replxx' ./_example
$ ./example
```

Please see [`_example/example.go`][example] for an overview of using the
`rline` package.

## Building

Both the [readline][] and [replxx][] libraries require static build artifacts
in the `${SRCDIR}/{readline,replxx}` directories. These can be built with the
included [`deps.sh`](deps.sh) script:

```sh
# build readline and replxx dependencies
$ cd /path/to/rline
$ ./deps.sh
```

### Build Tags

The [readline][] and [replxx][] libraries require CGO, and will not be included
in a build by default. Both or either can be included by specifying an
appropriate build tag:

```sh
# build with readline support
$ go build -tags rline_readline

# build with replxx support
$ go build -tags rline_replxx

# build with both readline and replxx support
$ go build -tags 'rline_readline rline_replxx'
```

#### Dynamic

The [readline][] prompt can be linked against a system's standard or packaged
readline library by using the `rline_readline_dynamic` tag:

```sh
# build with dynamic readline
$ go build -tags 'rline_readline_dynamic'
```

## Project Goals

Aims to provide a drop in replacement with _comparable_ feature support to
[GNU's readline][readline] library in pure Go, including (but not limited to):

1. `inputrc` support (or similar)
2. Comprehensive Windows support
3. Tab completion

And to do so with a Go-idiomatic API that allows runtime switching between
prompts.

Additionally, an aim of this project is to provide higher-level functionality
not available in the standard [readline][] library:

1. Syntax highlighting
2. Completion menus

## About

`rline` was built primarily to support these projects:

* [usql][usql] - a universal command-line interface for SQL databases

[example]: _example/example.go
[readline]: https://tiswww.case.edu/php/chet/readline/rltop.html
[replxx]: https://github.com/AmokHuginnsson/replxx
[usql]: https://github.com/xo/usql
