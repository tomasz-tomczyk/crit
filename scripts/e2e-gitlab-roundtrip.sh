#!/usr/bin/env bash
# End-to-end test runner for the crit ↔ GitLab MR roundtrip.
set -euo pipefail

CRIT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

if [ -z "${CRIT_GITLAB_ROUNDTRIP_PROJECT:-}" ]; then
  echo "✗ CRIT_GITLAB_ROUNDTRIP_PROJECT not set." >&2
  echo "  Export it (for example crit-md/crit-roundtrip-sandbox)." >&2
  echo "  See test/roundtrip/README.md for setup and safety notes." >&2
  exit 1
fi
if ! command -v glab >/dev/null 2>&1; then
  echo "✗ glab not installed" >&2
  exit 1
fi
if [ -n "${CRIT_GITLAB_ROUNDTRIP_HOST:-}" ]; then
  auth_status=(glab auth status --hostname "$CRIT_GITLAB_ROUNDTRIP_HOST")
else
  auth_status=(glab auth status)
fi
if ! "${auth_status[@]}" >/dev/null 2>&1; then
  echo "✗ glab not authenticated" >&2
  exit 1
fi

cd "$CRIT_DIR"
export CRIT_BINARY="$CRIT_DIR/crit"
exec go test -tags e2e_gitlab -count=1 -timeout 20m -run TestGitLabRoundtrip "$@" ./internal/session
