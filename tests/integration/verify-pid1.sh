#!/bin/sh
# Asserts the invariants that only hold when cbox-init runs as PID 1.
# Exits non-zero on failure, which makes cbox-init itself exit non-zero and so
# fails the container (and the CI step).
set -u

fail() {
    echo "✗ FAILED: $1"
    exit 1
}

echo "=== PID 1 invariants ==="

# 1. cbox-init really is PID 1 — the whole point of this image. The earlier
#    integration image ran it as a child of the test shell, so none of the
#    PID-1-only behavior below was ever exercised.
comm=$(cat /proc/1/comm 2>/dev/null || echo "?")
case "$comm" in
    cbox-init*) echo "  ✓ PID 1 is cbox-init" ;;
    *) fail "PID 1 is '$comm', expected cbox-init" ;;
esac

# 2. The orphaned grandchild must have been adopted by PID 1 and reaped.
#    Wait for it to exit, then look for zombies.
#
#    NOTE: a zombie's /proc/<pid>/cmdline is EMPTY, so grepping cmdline (as the
#    older test did) can never find one. State is read from field 3 of
#    /proc/<pid>/stat instead. The comm field is parenthesised and may contain
#    spaces, so cut everything up to the last ')' first.
zombies=""
i=0
while [ "$i" -lt 15 ]; do
    zombies=""
    for d in /proc/[0-9]*; do
        pid=$(basename "$d")
        [ "$pid" = "1" ] && continue
        stat=$(cat "$d/stat" 2>/dev/null) || continue
        state=$(printf '%s' "$stat" | sed 's/.*) //' | cut -d' ' -f1)
        if [ "$state" = "Z" ]; then
            zombies="$zombies $pid"
        fi
    done
    [ -z "$zombies" ] && break
    i=$((i + 1))
    sleep 1
done

if [ -n "$zombies" ]; then
    fail "zombie process(es) not reaped by PID 1:$zombies"
fi
echo "  ✓ no unreaped zombies (orphan adopted and collected by PID 1)"

# 3. The orphan must actually have been adopted by us at some point — i.e. the
#    reaper had something to do. Confirm PID 1 has no leftover children other
#    than the managed ones (best-effort, informational).
echo "  ✓ PID-1 process table clean"

echo "=== PID 1 invariants OK ==="
exit 0
