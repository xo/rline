#!/bin/sh
# Build the isocline demo program, which the capture harness records.
#
# The build writes to .build/ and never writes inside isocline/, because
# isocline/ is a separate git repository that this project does not change.
#
# isocline/src/isocline.c includes every other source file, so one gcc command
# builds the whole library.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
src="$root/isocline"
out="$root/.build"

if [ ! -f "$src/src/isocline.c" ]; then
  echo "no isocline source at $src" >&2
  echo "clone https://github.com/daanx/isocline into $src" >&2
  exit 1
fi

mkdir -p "$out"
# No -Wall. The unity build reports every unused static function, which is
# inherent to including all sources into one translation unit.
${CC:-gcc} -std=c99 -O2 \
  -I"$src/include" \
  -o "$out/example" \
  "$src/src/isocline.c" \
  "$src/test/example.c"

echo "built $out/example"
