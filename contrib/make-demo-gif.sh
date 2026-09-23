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
# A large font rather than a large picture. The terminal's default gives an
# 80x24 window about 480 pixels wide, and enlarging that afterwards is the one
# thing that cannot be done well: terminal glyphs are pixel exact, so any
# resampling smears every stem. At size 20 the same 80 by 24 is about 1284
# pixels wide and every one of them is a pixel the terminal drew.
echo "== recording"
go run ./uitest -terminal foot -session demo -video -font-size 20

mp4=uitest/out/session.mp4
gif=uitest/out/rline.gif
[[ -f "$mp4" ]] || { echo "no recording at $mp4; is wf-recorder installed?" >&2; exit 1; }

# The window is centred on the compositor's 1920x1080 output. Its size is read
# from a screenshot the run just took rather than assumed, because it depends
# on the font and on what the terminal makes of the requested size.
shot=$(ls uitest/out/foot/demo/*.png | tail -1)
read -r ww wh < <(python3 -c "
import struct,sys
d=open(sys.argv[1],'rb').read()
w,h=struct.unpack('>II', d[16:24]); print(w,h)" "$shot")

# Only the rows that hold anything are kept: a terminal with two thirds of it
# blank under the prompt makes a poor picture. Eight rows of the twenty-four.
w=$ww
h=$(( wh * 8 / 24 ))
x=$(( (1920 - ww) / 2 ))
y=$(( (1080 - wh) / 2 ))
echo "== window ${ww}x${wh}, keeping ${w}x${h}"

echo "== converting"
# No scaling at all. An earlier version of this enlarged 480 pixels to 720
# with lanczos, which is a 1.5x resample of pixel-exact glyphs, and the result
# was legible but fuzzy — every stem softened and every dim grey edge smeared.
# The font is what makes the picture large now, so the frames are used at the
# size the terminal drew them.
#
# Two passes: the first works out a palette for these frames, the second uses
# it. The full 256 rather than 96, because a GIF may have them and the dimmed
# help text banded visibly at 96. No dithering either: dithering trades
# banding for a stippled texture, and on flat terminal colours there is no
# banding left to trade once the palette is large enough.
ffmpeg -v error -y -i "$mp4" \
  -vf "crop=$w:$h:$x:$y,fps=15,palettegen=max_colors=256:stats_mode=full" \
  /tmp/rline-palette.png
ffmpeg -v error -y -i "$mp4" -i /tmp/rline-palette.png \
  -lavfi "crop=$w:$h:$x:$y,fps=15[x];[x][1:v]paletteuse=dither=none:diff_mode=rectangle" \
  -loop 0 "$gif"
rm -f /tmp/rline-palette.png

printf '== %s, %s KB\n' "$gif" "$(( $(stat -c%s "$gif") / 1024 ))"
