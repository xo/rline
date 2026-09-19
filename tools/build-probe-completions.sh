#!/bin/sh
# Build the probe that prints what isocline's completions.c and completers.c return.
#
# The build writes to .build/ and never writes inside isocline/, because
# isocline/ is a separate git repository that this project does not change.
#
# The probe includes isocline/src/isocline.c, which is the unity build of the
# whole library, so it reaches the static functions in completions.c and completers.c.
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
  -o "$out/probe-completions" \
  "$root/tools/probe-completions.c"

echo "built $out/probe-completions"
