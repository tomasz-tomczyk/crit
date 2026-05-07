#!/usr/bin/env bash
set -euo pipefail

PORT="${1:-3128}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
# Resolve symlinks / convert MSYS-style paths to native Windows paths.
DIR=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
trap 'rm -rf "$DIR" "$BIN_DIR" "${FAKE_HOME:-}"' EXIT

cd "$DIR"
git init -q -b main
git config user.email "test@test.com"
git config user.name "Test"

# Seed commit on main.
echo "seed" > seed.txt
git add seed.txt
git commit -qm "seed"
MAIN_SHA=$(git rev-parse HEAD)

# Stack: A -> B -> C, each with its own branch label so the picker's stack
# detection has a name to show for each level. HEAD ends up on feat-c so
# the picker walks feat-c → feat-b → feat-a → main.
git checkout -qb feat-a
echo "alpha" > a.txt
git add a.txt
git commit -qm "feat A"
A_SHA=$(git rev-parse HEAD)

git checkout -qb feat-b
echo "beta" > b.txt
git add b.txt
git commit -qm "feat B"
B_SHA=$(git rev-parse HEAD)

git checkout -qb feat-c
echo "gamma" > c.txt
git add c.txt
git commit -qm "feat C"
C_SHA=$(git rev-parse HEAD)

# feat-d is a SIBLING branch off B that rewrites b.txt. We don't switch
# HEAD to it — it stays out of the picker's stack walk so the stack tests
# see feat-a/feat-b/feat-c only. comment-stale.rangemode.spec.ts uses
# feat-d's tip as a "head moved" target via POST /api/focus.
git branch feat-d "$B_SHA"
git checkout -q feat-d
echo "completely different content" > b.txt
git add b.txt
git commit -qm "feat D: rewrite b.txt"
D_SHA=$(git rev-parse HEAD)
# Move HEAD back to feat-c for the picker tests.
git checkout -q feat-c

# Build crit binary outside the repo (skip if CRIT_BIN is set).
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  if command -v mise >/dev/null 2>&1; then
    (cd "$CRIT_SRC" && mise exec -- go build -o "$CRIT_BIN" .)
  else
    (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
  fi
fi

# Isolate from the user's ~/.crit.config.json (and USERPROFILE on Windows).
FAKE_HOME=$(e2e_native_tempdir)
e2e_export_fake_home "$FAKE_HOME"

# Write fixture state for E2E tests.
STATE_FILE="$(e2e_state_file "$PORT")"
{
  echo "CRIT_BIN=$CRIT_BIN"
  echo "CRIT_FIXTURE_DIR=$DIR"
  echo "FAKE_HOME=$FAKE_HOME"
  echo "RANGE_BASE=$A_SHA"
  echo "RANGE_HEAD=$B_SHA"
  echo "RANGE_DEFAULT=$MAIN_SHA"
  echo "RANGE_HEAD_AFTER=$D_SHA"
} > "$STATE_FILE"

# Boot crit in range mode A..B.
exec "$CRIT_BIN" _serve --no-open --port "$PORT" --range "$A_SHA..$B_SHA"
