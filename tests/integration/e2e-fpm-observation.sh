#!/usr/bin/env bash
# Real FPM/OPcache transport and idle test, not a CRIU acceptance test.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
IMAGE="${CBOX_TEST_PHP_IMAGE:-ghcr.io/cboxdk/php-baseimages/php-fpm:8.4-bookworm}"
if [ -n "${CBOX_TEST_FPM_EXPORTER_BINARY:-}" ]; then
    test -x "$CBOX_TEST_FPM_EXPORTER_BINARY"
    printf 'Testing exporter binary: '
    shasum -a 256 "$CBOX_TEST_FPM_EXPORTER_BINARY"
fi
FIXTURE="$(mktemp -d "${TMPDIR:-/tmp}/cbox-observation.XXXXXXXX")"
CONTAINER=""
cleanup() {
    if [ -n "$CONTAINER" ]; then docker rm -f "$CONTAINER" >/dev/null; fi
    rm -rf "$FIXTURE"
}
trap cleanup EXIT
chmod 755 "$FIXTURE"
cat > "$FIXTURE/fpm.conf" <<'CONF'
[global]
error_log = /proc/self/fd/2
daemonize = no
[www]
user = www-data
group = www-data
listen = 0.0.0.0:9000
pm = static
pm.max_children = 2
pm.status_path = /status
catch_workers_output = yes
CONF
# Map the probe directory at the identical path so the shared phpfpm library's
# temporary script is accessible to FPM without exposing the host's whole /tmp.
CONTAINER="$(docker run -d --entrypoint php-fpm \
    -p 127.0.0.1::9000 \
    --mount "type=bind,source=$FIXTURE,target=$FIXTURE,readonly" \
    "$IMAGE" -y "$FIXTURE/fpm.conf")"
UPSTREAM="$(docker port "$CONTAINER" 9000/tcp)"
printf 'Testing PHP image: %s\n' "$(docker inspect --format '{{.Image}}' "$CONTAINER")"
cd "$ROOT"
TMPDIR="$FIXTURE" CBOX_TEST_FPM_UPSTREAM="$UPSTREAM" \
    go test -race -tags integration -run '^TestObservation(WithRealFPM|ResponseDoesNotProveFPMQuiescence)$' -count=1 ./internal/snapshot \
    || { docker logs "$CONTAINER"; exit 1; }

printf '%s\n' 'Transport and FPM quiescence checks passed; actual CRIU safety is NOT established.'
