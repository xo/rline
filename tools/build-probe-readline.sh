#!/bin/sh
# Build the probe that prints what GNU readline does to a line for each key.
#
# It links the GNU readline of the system, through its public API, and needs
# its headers: readline 8.3 here, from the readline package. macOS ships
# libedit rather than GNU readline, so the recordings are made on Linux. The
# build writes to .build/, and the probe is never part of the Go module.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out="$root/.build"

mkdir -p "$out"
${CC:-gcc} -std=c99 -O2 -Wall \
  -o "$out/probe-readline" \
  "$root/tools/probe-readline.c" \
  -lreadline

echo "built $out/probe-readline"
