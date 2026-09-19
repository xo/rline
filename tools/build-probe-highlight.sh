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
# No -Wall. The unity build reports every unused static function.
${CC:-gcc} -std=c99 -O2 \
  -I"$src/include" -I"$src/src" \
  -o "$out/probe-highlight" \
  "$root/tools/probe-highlight.c"

echo "built $out/probe-highlight"
