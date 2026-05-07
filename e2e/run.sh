#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
GIT_PORT="${CRIT_TEST_PORT:-3123}"
FILE_PORT="${CRIT_TEST_FILE_PORT:-3124}"
SINGLE_PORT="${CRIT_TEST_SINGLE_PORT:-3125}"
NOGIT_PORT="${CRIT_TEST_NOGIT_PORT:-3126}"
MULTI_PORT="${CRIT_TEST_MULTI_PORT:-3127}"
RANGE_PORT="${CRIT_TEST_RANGE_PORT:-3128}"

# Build crit once (skip if CRIT_BIN already points to an existing binary, e.g. CI coverage builds)
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  BIN_DIR=$(mktemp -d)
  trap 'rm -rf "$BIN_DIR"' EXIT
  export CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" .)
fi

# Kill any stale processes on our test ports before starting fresh
for port in "$GIT_PORT" "$FILE_PORT" "$SINGLE_PORT" "$NOGIT_PORT" "$MULTI_PORT" "$RANGE_PORT"; do
  e2e_kill_port "$port"
done

# Start both fixture servers in parallel
cd "$SCRIPT_DIR"
bash setup-fixtures.sh "$GIT_PORT" &
GIT_PID=$!
bash setup-fixtures-filemode.sh "$FILE_PORT" &
FILE_PID=$!
bash setup-fixtures-singlefile.sh "$SINGLE_PORT" &
SINGLE_PID=$!
bash setup-fixtures-nogit.sh "$NOGIT_PORT" &
NOGIT_PID=$!
bash setup-fixtures-multifile.sh "$MULTI_PORT" &
MULTI_PID=$!
bash setup-fixtures-range-mode.sh "$RANGE_PORT" &
RANGE_PID=$!

cleanup() {
  kill "$GIT_PID" "$FILE_PID" "$SINGLE_PID" "$NOGIT_PID" "$MULTI_PID" "$RANGE_PID" 2>/dev/null || true
  wait "$GIT_PID" "$FILE_PID" "$SINGLE_PID" "$NOGIT_PID" "$MULTI_PID" "$RANGE_PID" 2>/dev/null || true
  # On Git Bash `kill <bash-pid>` doesn't reap the spawned crit.exe child;
  # taskkill /T flushes the whole tree.
  e2e_kill_stray_crit
  rm -rf "${BIN_DIR:-}"
}
trap cleanup EXIT

# Wait for servers to be ready
for port in "$GIT_PORT" "$FILE_PORT" "$SINGLE_PORT" "$NOGIT_PORT" "$MULTI_PORT" "$RANGE_PORT"; do
  while ! curl -sf "http://localhost:$port/api/session" >/dev/null 2>&1; do
    sleep 0.1
  done
done

# Run tests
if [ $# -eq 0 ]; then
  # No args: run all 4 projects in parallel for speed
  PWLOGS=$(mktemp -d)
  FAILED=0

  npx playwright test --project=git-mode > "$PWLOGS/git.log" 2>&1 &
  PW1=$!
  npx playwright test --project=file-mode > "$PWLOGS/file.log" 2>&1 &
  PW2=$!
  npx playwright test --project=single-file-mode > "$PWLOGS/single.log" 2>&1 &
  PW3=$!
  npx playwright test --project=no-git-mode > "$PWLOGS/nogit.log" 2>&1 &
  PW4=$!
  npx playwright test --project=multi-file-mode > "$PWLOGS/multi.log" 2>&1 &
  PW5=$!
  npx playwright test --project=range-mode > "$PWLOGS/range.log" 2>&1 &
  PW6=$!

  wait $PW1 || FAILED=1
  wait $PW2 || FAILED=1
  wait $PW3 || FAILED=1
  wait $PW4 || FAILED=1
  wait $PW5 || FAILED=1
  wait $PW6 || FAILED=1

  # Print results — show summary for passing projects, full output for failures
  for f in "$PWLOGS"/*.log; do
    name=$(basename "$f" .log)
    if grep -q "failed" "$f"; then
      echo "=== $name (FAILED) ==="
      # Dump the full project log on failure so CI shows every error message
      # (a 30-line tail buries per-test errors when many tests fail).
      cat "$f"
    else
      echo "=== $name ==="
      tail -5 "$f"
    fi
    echo
  done

  rm -rf "$PWLOGS"
  if [ $FAILED -ne 0 ]; then
    echo "Some projects failed. Run 'make e2e-failed' or check individual project logs."
    exit 1
  fi
else
  # Custom args passed: run sequentially as-is
  npx playwright test "$@"
fi
