#!/bin/sh
# Build the probe that prints what isocline's attr.c and color helpers return.
#
# The build writes to .build/ and never writes inside isocline/.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
src="$root/isocline"
out="$root/.build"

if [ ! -f "$src/src/isocline.c" ]; then
  echo "no isocline source at $src" >&2
  exit 1
fi

mkdir -p "$out"
# -DNDEBUG on purpose. edit_refresh asserts that the rows of the extra content
# start at or after zero, and that fails whenever the line is long enough to be
# clipped to the height of the terminal while there is extra content to show,
# which is reachable with a completion menu on a short terminal. A release
# build, which is what people run, compiles the assertion out and draws no
# extra rows. That is the behaviour the port has to match.
#
# No -Wall. The unity build reports every unused static function.
${CC:-gcc} -std=c99 -O2 -DNDEBUG \
  -I"$src/include" -I"$src/src" \
  -o "$out/probe-completion-menu" \
  "$root/tools/probe-completion-menu.c"

echo "built $out/probe-completion-menu"
