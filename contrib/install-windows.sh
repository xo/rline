#!/usr/bin/env bash
# Install everything uitest drives on Windows.
#
# Written for bash with coreutils — Git Bash, MSYS2 or WSL with winget on the
# path. Safe to run again: winget is told to skip anything already installed.
#
# conhost.exe is part of Windows and is not installed here. It is also the
# half of the matrix that matters most, being the host that has historically
# mishandled cursor shapes and lacks synchronized output.

set -euo pipefail

if ! command -v winget.exe >/dev/null 2>&1 && ! command -v winget >/dev/null 2>&1; then
  echo "winget is not on the path." >&2
  echo "It ships with modern Windows as App Installer; from WSL you may need" >&2
  echo "to run this from a Windows shell instead." >&2
  exit 1
fi

winget_cmd=$(command -v winget.exe || command -v winget)

# Package identifiers rather than search terms, so that this installs what it
# says and not whatever happens to match a name today.
packages=(
  "wez.wezterm"                  # the one terminal that runs on all three platforms
  "Microsoft.WindowsTerminal"    # the default host on Windows 11
)

echo "== terminals"
for p in "${packages[@]}"; do
  echo "   $p"
  # --accept-*-agreements so this does not stop on a prompt in a script, and
  # a package already present makes winget exit non-zero, which is not a
  # failure of this script.
  "$winget_cmd" install --id "$p" --exact --silent \
    --accept-package-agreements --accept-source-agreements || {
      echo "   (already installed, or winget declined; carrying on)"
    }
done

echo
echo "conhost.exe is part of Windows and needs nothing."
echo "PowerShell, which uitest drives for keys and screenshots, is part of"
echo "Windows too."
echo
echo "Next: go run ./uitest -list"
echo
echo "Note that uitest tests hosts rather than shells. cmd.exe, pwsh and"
echo "powershell are shells; what encodes the keys and draws the screen is"
echo "conhost or Windows Terminal, and that is what the matrix names."
