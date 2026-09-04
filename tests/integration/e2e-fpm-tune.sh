#!/usr/bin/env bash
#
# End-to-end test for the embedded runtime PHP-FPM autotuner (global.fpm_tune).
#
# Runs cbox-init as PID 1 in a php:8.4-fpm container, supervising a real php-fpm
# with the fpm-tune loop in apply mode, and asserts the full chain:
#
#   discover www -> enable its status page -> scrape (PSS) -> size ->
#   apply a resize under load by reloading php-fpm with SIGUSR2 (never a restart).
#
# The driver is external (docker exec / curl from the host) because php-fpm is a
# longrun process, so the oneshot-verify pattern (Dockerfile.pid1) does not apply.
# Exits non-zero on the first failed assertion, which fails the CI step.
set -euo pipefail

IMAGE=cbox-init-fpmtune-e2e
NAME=cbox-init-fpmtune-e2e
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() {
    echo "✗ FAILED: $1"
    echo "---- container logs (tail) ----"
    docker logs "$NAME" 2>&1 | tail -40 || true
    exit 1
}

# make build-all (local) or the CI artifact must have produced the linux binary
# the Dockerfile COPYs for this build platform.
[ -f "$ROOT/build/cbox-init-linux-amd64" ] || [ -f "$ROOT/build/cbox-init-linux-arm64" ] \
    || fail "build/cbox-init-linux-* missing; run 'make build-all' first"

echo "=== build image ==="
docker build -f "$ROOT/tests/integration/Dockerfile.fpmtune" -t "$IMAGE" "$ROOT" >/dev/null

echo "=== run cbox-init as PID 1 (memory-bounded for a predictable budget) ==="
cleanup
docker run -d --name "$NAME" --memory=512m -p 9110:9110 "$IMAGE" >/dev/null

# One www-pool metric value, or empty until the loop has scraped it once.
metric() { curl -sf --max-time 5 http://localhost:9110/metrics 2>/dev/null | awk -v k="$1" '$1==k{print $2}'; }

# The php-fpm master pid, found via /proc without pgrep/ps (minimal image).
master_pid() {
    docker exec "$NAME" sh -c '
        for p in /proc/[0-9]*/; do
            c=$(tr "\0" " " < "$p/cmdline" 2>/dev/null) || continue
            case "$c" in *"master process"*) echo "$p" | tr -dc "0-9"; return;; esac
        done'
}

echo "=== the loop discovers www and serves metrics ==="
ready=""
for _ in $(seq 1 40); do
    [ -n "$(metric 'fpm_tune_pool_workers_configured{pool="www"}')" ] && { ready=1; break; }
    sleep 1
done
[ -n "$ready" ] || fail "the loop never discovered the www pool on /metrics"
echo "  ✓ www discovered; fpm_tune_* metrics served"

echo "=== apply enabled the status page (drop-in written) ==="
docker exec "$NAME" test -f /usr/local/etc/php-fpm.d/zz-fpm-tune-status.conf \
    || fail "status drop-in zz-fpm-tune-status.conf was not written"
echo "  ✓ zz-fpm-tune-status.conf present"

configured0=$(metric 'fpm_tune_pool_workers_configured{pool="www"}')
master0=$(master_pid)
[ -n "$master0" ] || fail "could not find the php-fpm master pid"
echo "  ✓ baseline: configured=$configured0, php-fpm master pid=$master0"

echo "=== drive saturating load; the loop must resize the pool ==="
docker exec -d "$NAME" sh -c '
    end=$(( $(date +%s) + 150 ))
    while [ $(date +%s) -lt $end ]; do
        i=0
        while [ $i -lt 25 ]; do
            ( SCRIPT_FILENAME=/var/www/html/busy.php SCRIPT_NAME=/busy.php \
              REQUEST_METHOD=GET QUERY_STRING= \
              cgi-fcgi -bind -connect 127.0.0.1:9000 >/dev/null 2>&1 ) &
            i=$((i + 1))
        done
        wait
    done'

resized=""
for _ in $(seq 1 150); do
    c=$(metric 'fpm_tune_pool_workers_configured{pool="www"}')
    r=$(metric 'fpm_tune_pool_workers_recommended{pool="www"}')
    if [ -n "$c" ] && [ "$c" -gt "$configured0" ]; then
        resized=1
        echo "  ✓ resized under load: configured $configured0 -> $c (recommended $r)"
        break
    fi
    sleep 1
done
[ -n "$resized" ] || fail "no resize under load (configured stayed at $configured0)"

echo "=== the resize wrote a pm.max_children drop-in ==="
docker exec "$NAME" sh -c 'grep -q "pm.max_children" /usr/local/etc/php-fpm.d/zz-fpm-tune.conf' \
    || fail "pm drop-in zz-fpm-tune.conf with pm.max_children was not written"
echo "  ✓ zz-fpm-tune.conf carries pm.max_children"

echo "=== the reload was SIGUSR2, not a restart (master pid unchanged) ==="
master1=$(master_pid)
[ -n "$master1" ] && [ "$master0" = "$master1" ] \
    || fail "php-fpm master pid changed ($master0 -> ${master1:-gone}): a restart, not a graceful reload"
echo "  ✓ master pid $master1 unchanged across the resize"

echo "=== graceful shutdown stops the loop before php-fpm drains ==="
docker stop -t 15 "$NAME" >/dev/null
code=$(docker inspect -f '{{.State.ExitCode}}' "$NAME")
[ "$code" = "0" ] || fail "graceful shutdown exited $code, expected 0"
echo "  ✓ graceful shutdown exit 0"

echo "=== E2E PASSED: init embeds and drives fpm-tune end-to-end ==="
