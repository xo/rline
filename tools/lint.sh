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
#
# The built linter is cached under .build/, which is inside the repository
# and gitignored, rather than under the temporary directory. That is not
# tidiness. TMPDIR is unset on most Linux systems, so the cache landed in
# /tmp, which is world-writable: the script ran whatever executable happened
# to be at that path, so any other user on the machine could leave one there
# and have it run as whoever lints next. Demonstrated rather than supposed —
# a shell script left at the cache path ran in place of the linter.
#
# The name carries the system, the architecture and a checksum of the pin, so
# a checkout shared between machines does not run the wrong binary and a
# branch with a different pin does not reuse the old one. Keying on the
# modification time would not do the second: checking out an older branch can
# leave the pin older than the binary built from a newer one.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
mod=$root/tools/lint

# cksum is in POSIX and is on every system this builds for. It is a cache
# key, not a signature: the directory it guards is the developer's own.
key=$(cat "$mod/go.mod" "$mod/go.sum" | cksum | tr -d ' \t' ) || exit 2
arch=$(go env GOOS)-$(go env GOARCH) || exit 2
dir=$root/.build
bin=$dir/golangci-lint-$arch-$key

if [ ! -x "$bin" ]; then
    echo "building the pinned linter..." >&2
    mkdir -p "$dir" || exit 2
    # Built beside the final name and moved into place, so that two runs at
    # once cannot execute a half-written binary.
    tmp=$bin.$$
    go -C "$mod" build -o "$tmp" github.com/golangci/golangci-lint/v2/cmd/golangci-lint || { rm -f "$tmp"; exit 2; }
    mv -f "$tmp" "$bin" || { rm -f "$tmp"; exit 2; }
    # Older builds of other pins are no longer reachable, so they go. The
    # pattern leaves the probes beside them, and leaves anything ending in
    # .exe, which is lint.ps1's cache: the two scripts key the pin
    # differently, so each cleans up only its own names. Without that, a
    # machine that runs both rebuilds on every alternate run and says
    # nothing about it.
    find "$dir" -maxdepth 1 -name 'golangci-lint-*' ! -name '*.exe' \
        ! -name "$(basename "$bin")" -exec rm -f {} + 2>/dev/null
fi
cd "$root" || exit 2
exec "$bin" run "$@"
