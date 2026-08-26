#!/usr/bin/env bash
#
# check-doc-configs.sh — validate the shipped example configs against the real
# schema, so documented configuration cannot drift from what the binary accepts.
#
# It runs `cbox-init check-config` over every cbox-init config in
# configs/examples/. Those files back the examples throughout docs/, so a config
# that references a removed or misspelled key fails here instead of misleading a
# reader.
#
# Deployment manifests that are NOT cbox-init configs (docker-compose, k8s) live
# under deploy/ and are intentionally not matched by the glob below.
#
# Usage:
#   tools/check-doc-configs.sh          # builds the binary if needed, validates
#   BIN=./build/cbox-init tools/check-doc-configs.sh
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

BIN="${BIN:-build/cbox-init}"
if [ ! -x "$BIN" ]; then
  echo "==> building $BIN"
  make build >/dev/null
fi

shopt -s nullglob
configs=(configs/examples/*.yaml)
if [ ${#configs[@]} -eq 0 ]; then
  echo "no configs found under configs/examples/*.yaml" >&2
  exit 1
fi

fail=0
for cfg in "${configs[@]}"; do
  if "$BIN" check-config --config "$cfg" --quiet >/tmp/cbox-doc-config.out 2>&1; then
    printf 'PASS  %s\n' "$cfg"
  else
    fail=1
    printf 'FAIL  %s\n' "$cfg"
    sed 's/^/      /' /tmp/cbox-doc-config.out
  fi
done

echo
if [ "$fail" -ne 0 ]; then
  echo "❌ one or more example configs failed validation"
  exit 1
fi
echo "✅ all ${#configs[@]} example configs are valid"
