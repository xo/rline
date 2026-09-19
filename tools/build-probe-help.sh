#!/bin/sh
# Build the probe that prints the lines isocline's help screen is built from.
#
# The build writes to .build/ and never writes inside isocline/, because
# isocline/ is a separate git repository that this project does not change.
#
# Three rows of the help name a different key on macOS, chosen with
# "#if __APPLE__", so there are two recordings to make. On macOS this builds a
# second probe with that branch turned off, which gives exactly the table the
# other build has: the probe reads only the help table, and nothing else in
# the C reaches it. So one machine can record both, unlike the redraw, where
# the recording comes from running the whole editor.
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
  -I"$src/include" -I"$src/src" \
  -o "$out/probe-help" \
  "$root/tools/probe-help.c"
echo "built $out/probe-help"

case "$(uname -s)" in
Darwin)
  ${CC:-gcc} -std=c99 -O2 -U__APPLE__ \
    -I"$src/include" -I"$src/src" \
    -o "$out/probe-help-default" \
    "$root/tools/probe-help.c"
  echo "built $out/probe-help-default (the branch turned off)"
  ;;
esac
