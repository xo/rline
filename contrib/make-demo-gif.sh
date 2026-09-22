#!/usr/bin/env bash
# Regenerate the demonstration in the top-level README.
#
# The recording is produced rather than taken by hand, so it can be made again
# after the code changes and always shows what rline does now. Needs Linux
# with a wlroots compositor, the uitest dependencies, and ffmpeg.
#
#   ./contrib/make-demo-gif.sh            writes uitest/out/rline.gif
#
# The result is NOT committed to this repository. A Go module zip contains
# every file in the tree, so a binary here is downloaded by everyone who runs
# go get, forever, and a regenerated one is a new blob in the history each
# time. Publish it on the orphan assets branch instead:
#
#   git switch --orphan assets
#   cp uitest/out/rline.gif .
#   git add rline.gif && git commit -m 'Demonstration for the README'
#   git push -u origin assets
#   git switch -
#
# The README points at
# https://raw.githubusercontent.com/xo/rline/assets/rline.gif, which is an
# absolute URL because pkg.go.dev does not reliably resolve relative image
# paths and renders the README for the package documentation page.

set -euo pipefail

cd "$(dirname "$0")/.."

for tool in ffmpeg go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$tool is needed and is not installed" >&2; exit 1; }
done

# foot starts fastest and its defaults are plain, which keeps the recording
# about rline rather than about somebody's terminal theme.
echo "== recording"
go run ./uitest -terminal foot -session demo -video

mp4=uitest/out/session.mp4
gif=uitest/out/rline.gif
[[ -f "$mp4" ]] || { echo "no recording at $mp4; is wf-recorder installed?" >&2; exit 1; }

# The window is centred on the compositor's 1920x1080 output at its opening
# size of 80x24 characters, which foot draws at 480x384 pixels. Only the rows
# that hold anything are kept: a terminal with seventeen blank rows under the
# prompt makes a poor picture.
w=480; h=208
x=$(( (1920 - 480) / 2 ))
y=$(( (1080 - 384) / 2 ))

echo "== converting"
# Two passes: the first works out a palette for these frames, the second uses
# it. One pass with the default palette gives visible banding on the dimmed
# help text, which is most of the picture.
ffmpeg -v error -y -i "$mp4" \
  -vf "crop=$w:$h:$x:$y,fps=12,scale=720:-1:flags=lanczos,palettegen=max_colors=96" \
  /tmp/rline-palette.png
ffmpeg -v error -y -i "$mp4" -i /tmp/rline-palette.png \
  -lavfi "crop=$w:$h:$x:$y,fps=12,scale=720:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
  -loop 0 "$gif"
rm -f /tmp/rline-palette.png

printf '== %s, %s KB\n' "$gif" "$(( $(stat -c%s "$gif") / 1024 ))"
