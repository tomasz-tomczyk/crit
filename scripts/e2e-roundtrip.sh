#!/usr/bin/env bash
# End-to-end integration test runner for the crit ↔ GitHub PR roundtrip.
# Builds crit, then runs Go tests behind build tag e2e_github against a real
# GitHub PR in the sandbox repo configured via CRIT_ROUNDTRIP_REPO.
#
# Usage:
#   ./scripts/e2e-roundtrip.sh                 # all scenarios
#   ./scripts/e2e-roundtrip.sh -run TestX      # one scenario
#   ./scripts/e2e-roundtrip.sh -v -count=1     # extra go test args
set -euo pipefail

CRIT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

if command -v /opt/homebrew/bin/mise >/dev/null 2>&1; then
  eval "$(/opt/homebrew/bin/mise env -s bash -C "$CRIT_DIR" 2>/dev/null)" || true
fi

if [ -z "${CRIT_ROUNDTRIP_REPO:-}" ]; then
  echo "✗ CRIT_ROUNDTRIP_REPO not set." >&2
  echo "  Export it (e.g. CRIT_ROUNDTRIP_REPO=crit-md/crit-roundtrip-sandbox)." >&2
  echo "  See test/roundtrip/README.md for one-time setup." >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "✗ gh not installed" >&2; exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "✗ gh not authenticated" >&2; exit 1
fi

echo "→ Building crit..."
make -C "$CRIT_DIR" build -j

export CRIT_ROUNDTRIP_REPO
export CRIT_BINARY="$CRIT_DIR/crit"

cd "$CRIT_DIR"
exec go test -tags e2e_github -count=1 -timeout 20m -run TestRoundtrip "$@" ./...
