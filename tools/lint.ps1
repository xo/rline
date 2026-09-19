# Run the pinned linter over the repository, the same as lint.sh.
#
# golangci-lint needs a newer Go than the library does, so it lives in a
# module of its own under tools/lint. That keeps the library buildable with
# the Go that FreeBSD ships, and it means the linter cannot be run with
# `go tool` from here: a tool in another module cannot see this one.
#
# So it is built first and then run from the top of the repository, which is
# where it has to stand to analyze the package.
#
# Written by the windows-vm session and then given the hardening that
# lint.sh had received an hour earlier, because the first version of both
# cached the binary in the temporary directory and ran whatever was at that
# path. On Windows GetTempPath is per user rather than world writable, so it
# is not the same hole, but the cache lives beside the bash one under .build/
# so that the two agree and neither has to be reasoned about separately.
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$mod = Join-Path $root 'tools/lint'

# The key is a hash of the pin rather than a timestamp. Checking out a branch
# with an older pin leaves that pin older than a binary built from a newer
# one, and a timestamp test would keep the wrong linter.
$pin = (Get-Content (Join-Path $mod 'go.mod') -Raw) + (Get-Content (Join-Path $mod 'go.sum') -Raw)
$sha = [BitConverter]::ToString(
    [Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($pin))
).Replace('-', '').Substring(0, 16).ToLower()
$arch = "$(& go env GOOS)-$(& go env GOARCH)"
$dir = Join-Path $root '.build'
$bin = Join-Path $dir "golangci-lint-$arch-$sha.exe"

if (-not (Test-Path $bin)) {
    [Console]::Error.WriteLine('building the pinned linter...')
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    # Built beside the final name and moved into place, so that two runs at
    # once cannot execute a half-written binary.
    #
    # The name is deliberately not golangci-lint-something.exe. It was
    # "$bin.$PID", which gives ...exe.1234 — the cleanup filter below does not
    # match that, so instead of the race lint.sh had, a failed build left a
    # temporary file behind for ever. ken-mba found both shapes. The finally
    # block takes it away however this run ends.
    #
    # Move-Item -Force onto a binary another process is running fails on
    # Windows rather than replacing it, so two runs at once end with one of
    # them stopping here. That is better than running a half-written binary,
    # and it is a hard stop rather than a retry.
    #
    # The message it gives is misleading, which is the part worth knowing:
    # "Cannot create a file when that file already exists." -Force deletes
    # the destination first, the running process refuses that delete, and
    # the rename then fails on a file that is still there. So it reports the
    # thing anyone can see rather than the cause. Measured on Windows by
    # windows-vm, who needed a cold cache to reproduce it, because a warm
    # run exits before it can be caught holding the file.
    $tmp = Join-Path $dir ".building-golangci-lint-$PID"
    try {
        & go -C $mod build -o $tmp github.com/golangci/golangci-lint/v2/cmd/golangci-lint
        if ($LASTEXITCODE -ne 0) { exit 2 }
        Move-Item -Force $tmp $bin
    } finally {
        Remove-Item -Force $tmp -ErrorAction SilentlyContinue
    }
    # Older builds of other pins are no longer reachable, so they go. The
    # pattern is the linter's own names ending in .exe, so the probes stay
    # and so does lint.sh's cache: the two key the pin differently, so each
    # cleans up only its own names.
    Get-ChildItem -Path $dir -Filter 'golangci-lint-*.exe' -File |
        Where-Object { $_.FullName -ne $bin } |
        Remove-Item -Force -ErrorAction SilentlyContinue
}
Set-Location $root
& $bin run @args
exit $LASTEXITCODE
