#!/usr/bin/env bash
# Install everything uitest drives on macOS.
#
# Terminal.app is part of the system and is not installed here. The rest come
# from Homebrew. Safe to run again: anything already present is left alone.
#
# After this, grant Accessibility permission to whatever terminal you run the
# tests from. macOS will ask the first time uitest types, and until it is
# granted every keystroke fails with an osascript error rather than going
# somewhere unexpected.

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "this is the macOS installer; on Linux use your package manager and on Windows use install-windows.sh" >&2
  exit 1
fi

if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is not installed. See https://brew.sh, then run this again." >&2
  exit 1
fi

# Casks are applications, formulae are command line programs. Both are named
# here rather than looked up, so that this script says exactly what it will
# put on the machine.
casks=(
  wezterm   # the one terminal that runs on all three platforms
  ghostty   # a modern engine with the kitty keyboard protocol
  iterm2    # what most Mac users actually run
)
formulae=(
  tmux      # a multiplexer in the middle, which rewrites TERM and filters keys
)

echo "== terminals"
for c in "${casks[@]}"; do
  if brew list --cask "$c" >/dev/null 2>&1; then
    echo "   $c already installed"
  else
    echo "   installing $c"
    brew install --cask "$c"
  fi
done

echo "== tools"
for f in "${formulae[@]}"; do
  if brew list --formula "$f" >/dev/null 2>&1; then
    echo "   $f already installed"
  else
    echo "   installing $f"
    brew install "$f"
  fi
done

echo
echo "Terminal.app is part of macOS and needs nothing."
echo "osascript and screencapture, which uitest drives, are part of macOS too."
echo
echo "Next: go run ./uitest -list"
echo "The first run will ask for Accessibility permission. It is needed to"
echo "synthesise keystrokes, and without it uitest refuses to type rather"
echo "than typing into the wrong window."
