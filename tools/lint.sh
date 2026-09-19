#!/bin/bash
# Run the pinned linter over the repository.
#
# golangci-lint needs a newer Go than the library does, so it lives in a
# module of its own under tools/lint. That keeps the library buildable with
# the Go that FreeBSD ships, and it means the linter cannot be run with
# `go tool` from here: a tool in another module cannot see this one.
#
# So it is built first and then run from the top of the repository, which is
# where it has to stand to analyze the package.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
bin=${TMPDIR:-/tmp}/rline-golangci-lint
if [ ! -x "$bin" ] || [ "$root/tools/lint/go.sum" -nt "$bin" ]; then
    echo "building the pinned linter..." >&2
    go -C "$root/tools/lint" build -o "$bin" github.com/golangci/golangci-lint/v2/cmd/golangci-lint || exit 2
fi
cd "$root" || exit 2
exec "$bin" run "$@"
