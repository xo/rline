#!/bin/bash
# Break the code on purpose and check that a test says so.
#
# Usage: tools/mutate.sh <file> <find> <replace> [test pattern]
#
# It answers one of three things, and the third is the point of it. "caught"
# means the test failed, which is what a mutation is for. "NOT CAUGHT" means
# the test passed with the code broken, which names either a missing case or a
# line nothing reads. "DID NOT COMPILE" means nothing was tested at all: go
# test returns non-zero for a build failure exactly as it does for a test
# failure, so a harness that only reads the return code reports a mutation as
# caught when the test never ran. That answer was found by the macOS session,
# seconding a mutation run from this one that had exactly that fault.
#
# Every step is asserted rather than assumed: that the patch applied, that the
# file changed, that it still builds, and that the file came back.
set -u
file=$1 find=$2 replace=$3 pattern=${4:-}
[ -f "$file" ] || { echo "no such file: $file"; exit 2; }

# A run owns the tree while it lasts. Anything else reading the tree meanwhile
# reads mutated code and reports failures that belong to neither test, which
# has now happened once here and twice to the Windows session with its own
# harness. A lock says so rather than leaving it to convention.
lock=.mutate.lock
if ! mkdir "$lock" 2>/dev/null; then
    echo "another mutation run holds $lock: wait for it, or remove the directory if it is stale"
    exit 2
fi
orig=$(mktemp) || exit 2
trap 'cp "$orig" "$file"; rm -f "$orig"; rmdir "$lock" 2>/dev/null' EXIT
cp "$file" "$orig" || exit 2

python3 - "$file" "$find" "$replace" <<'PY' || { echo "the text to replace is not in $file"; exit 2; }
import sys
path, find, replace = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
if find not in s:
    sys.exit(1)
open(path, 'w').write(s.replace(find, replace, 1))
PY
cmp -s "$file" "$orig" && { echo "the patch changed nothing"; exit 2; }

if ! go build ./... >/dev/null 2>&1; then
    echo "DID NOT COMPILE: nothing was tested, so this says nothing about the test"
    exit 1
fi
if [ -n "$pattern" ]; then
    go test -count=1 -run "$pattern" ./... >/dev/null 2>&1
else
    # No pattern means the whole package, which is the honest default: a
    # mutation only the rest of the suite catches says the test under
    # defence is narrower than it looks, but it is not a hole.
    go test -count=1 ./... >/dev/null 2>&1
fi
rc=$?
cp "$orig" "$file" || { echo "RESTORE FAILED"; exit 2; }
cmp -s "$file" "$orig" || { echo "RESTORE FAILED"; exit 2; }
[ $rc -ne 0 ] && echo "caught" || echo "NOT CAUGHT"
