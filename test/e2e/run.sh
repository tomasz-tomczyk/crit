#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
GIT_PORT="${CRIT_TEST_PORT:-3123}"
GIT2_PORT="${CRIT_TEST_GIT2_PORT:-3131}"
FILE_PORT="${CRIT_TEST_FILE_PORT:-3124}"
SINGLE_PORT="${CRIT_TEST_SINGLE_PORT:-3125}"
NOGIT_PORT="${CRIT_TEST_NOGIT_PORT:-3126}"
MULTI_PORT="${CRIT_TEST_MULTI_PORT:-3127}"
RANGE_PORT="${CRIT_TEST_RANGE_PORT:-3128}"
LIVE_PORT="${CRIT_TEST_LIVE_PORT:-3129}"
SHARE_PORT="${CRIT_TEST_SHARE_PORT:-3132}"
STUB_PORT="${CRIT_TEST_STUB_PORT:-3133}"
PERF_PORT="${CRIT_TEST_PERF_PORT:-3134}"
STUB2_PORT="${CRIT_TEST_STUB2_PORT:-3135}"
HUGE_PORT="${CRIT_TEST_HUGE_PORT:-3136}"

# E2E_SHARD splits the no-arg full run across CI matrix VMs while keeping
# every test running somewhere (no coverage is dropped). "1/2" runs the git
# first half + file/single/nogit/multi/huge; "2/2" runs the git second half +
# range/live/share/perf. Unset (default) runs everything: local runs and
# Linux CI are unchanged. Mobile always runs after git in the default path
# only (it shares git's fixture and server state).
SHARD="${E2E_SHARD:-all}"
START_GIT=1; START_GIT2=1; START_FILE=1; START_SINGLE=1; START_NOGIT=1
START_MULTI=1; START_RANGE=1; START_LIVE=1; START_SHARE=1; START_PERF=1
START_HUGE=1
if [ "$SHARD" = "1/2" ]; then
  # Runs git --shard=1/2 against the GIT_PORT fixture, so GIT2 is unneeded.
  # Huge lives here: its fixture (~2,500 files) is the slowest NTFS
  # generation, and shard 2 already carries live + share + perf setup.
  START_GIT2=0
  START_RANGE=0; START_LIVE=0; START_SHARE=0; START_PERF=0
elif [ "$SHARD" = "2/2" ]; then
  # Runs git --shard=2/2 against the GIT2_PORT fixture, so GIT is unneeded.
  START_GIT=0
  START_FILE=0; START_SINGLE=0; START_NOGIT=0; START_MULTI=0; START_HUGE=0
fi

# Pre-initialized: the cleanup trap references every PID, and `set -u`
# forbids unset variables, so shards that skip fixtures must still have
# (empty) values. kill/wait on "" fail silently into the trap's || true.
GIT_PID=""; GIT2_PID=""; FILE_PID=""; SINGLE_PID=""; NOGIT_PID=""
MULTI_PID=""; RANGE_PID=""; LIVE_PID=""; SHARE_PID=""; PERF_PID=""
HUGE_PID=""

# Build crit once (skip if CRIT_BIN already points to an existing binary, e.g. CI coverage builds)
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  BIN_DIR=$(mktemp -d)
  trap 'rm -rf "$BIN_DIR"' EXIT
  export CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# Ensure the Chromium build matching this project's pinned @playwright/test is
# installed. The browser cache (~/Library/Caches/ms-playwright on macOS) is
# global and version-specific, so a machine that only has another project's
# Playwright version (e.g. crit-web's) won't have the build crit needs and every
# test fails with "Executable doesn't exist". Idempotent — skips the download
# when the build is already cached, so it's a fast no-op in CI and on reruns.
(cd "$SCRIPT_DIR" && npx playwright install chromium)

# Kill any stale processes on our test ports before starting fresh
for port in "$GIT_PORT" "$GIT2_PORT" "$FILE_PORT" "$SINGLE_PORT" "$NOGIT_PORT" "$MULTI_PORT" "$RANGE_PORT" "$LIVE_PORT" "$SHARE_PORT" "$STUB_PORT" "$STUB2_PORT" "$PERF_PORT" "$HUGE_PORT"; do
  e2e_kill_port "$port"
done

# Start fixture servers in parallel — only the ones this shard's projects
# need, so a matrix shard doesn't pay for (or contend with) idle servers.
# Heavy fixtures (perf: 300 files, huge: ~2,500 files) are slow to generate
# on Windows NTFS, so skipping them per shard is a real saving.
cd "$SCRIPT_DIR"
if [ "$START_GIT" = 1 ]; then bash setup-fixtures.sh "$GIT_PORT" & GIT_PID=$!; fi
if [ "$START_GIT2" = 1 ]; then bash setup-fixtures.sh "$GIT2_PORT" & GIT2_PID=$!; fi
if [ "$START_FILE" = 1 ]; then bash setup-fixtures-filemode.sh "$FILE_PORT" & FILE_PID=$!; fi
if [ "$START_SINGLE" = 1 ]; then bash setup-fixtures-singlefile.sh "$SINGLE_PORT" & SINGLE_PID=$!; fi
if [ "$START_NOGIT" = 1 ]; then bash setup-fixtures-nogit.sh "$NOGIT_PORT" & NOGIT_PID=$!; fi
if [ "$START_MULTI" = 1 ]; then bash setup-fixtures-multifile.sh "$MULTI_PORT" & MULTI_PID=$!; fi
if [ "$START_RANGE" = 1 ]; then bash setup-fixtures-range-mode.sh "$RANGE_PORT" & RANGE_PID=$!; fi
if [ "$START_LIVE" = 1 ]; then bash setup-fixtures-livemode.sh "$LIVE_PORT" & LIVE_PID=$!; fi
if [ "$START_SHARE" = 1 ]; then bash setup-fixtures-sharetransport.sh "$SHARE_PORT" "$STUB_PORT" "$STUB2_PORT" & SHARE_PID=$!; fi
if [ "$START_PERF" = 1 ]; then bash setup-fixtures-perf.sh "$PERF_PORT" & PERF_PID=$!; fi
if [ "$START_HUGE" = 1 ]; then bash setup-fixtures-huge.sh "$HUGE_PORT" & HUGE_PID=$!; fi

cleanup() {
  kill "$GIT_PID" "$GIT2_PID" "$FILE_PID" "$SINGLE_PID" "$NOGIT_PID" "$MULTI_PID" "$RANGE_PID" "$LIVE_PID" "$SHARE_PID" "$PERF_PID" "$HUGE_PID" 2>/dev/null || true
  wait "$GIT_PID" "$GIT2_PID" "$FILE_PID" "$SINGLE_PID" "$NOGIT_PID" "$MULTI_PID" "$RANGE_PID" "$LIVE_PID" "$SHARE_PID" "$PERF_PID" "$HUGE_PID" 2>/dev/null || true
  # On Git Bash `kill <bash-pid>` doesn't reap the spawned crit.exe child;
  # taskkill /T flushes the whole tree.
  e2e_kill_stray_crit
  rm -rf "${BIN_DIR:-}"
}
trap cleanup EXIT

# Wait for servers to be ready — only the ones this shard started.
READY_PORTS=""
if [ "$START_GIT" = 1 ]; then READY_PORTS="$READY_PORTS $GIT_PORT"; fi
if [ "$START_GIT2" = 1 ]; then READY_PORTS="$READY_PORTS $GIT2_PORT"; fi
if [ "$START_FILE" = 1 ]; then READY_PORTS="$READY_PORTS $FILE_PORT"; fi
if [ "$START_SINGLE" = 1 ]; then READY_PORTS="$READY_PORTS $SINGLE_PORT"; fi
if [ "$START_NOGIT" = 1 ]; then READY_PORTS="$READY_PORTS $NOGIT_PORT"; fi
if [ "$START_MULTI" = 1 ]; then READY_PORTS="$READY_PORTS $MULTI_PORT"; fi
if [ "$START_RANGE" = 1 ]; then READY_PORTS="$READY_PORTS $RANGE_PORT"; fi
if [ "$START_LIVE" = 1 ]; then READY_PORTS="$READY_PORTS $LIVE_PORT"; fi
if [ "$START_SHARE" = 1 ]; then READY_PORTS="$READY_PORTS $SHARE_PORT"; fi
if [ "$START_PERF" = 1 ]; then READY_PORTS="$READY_PORTS $PERF_PORT"; fi
if [ "$START_HUGE" = 1 ]; then READY_PORTS="$READY_PORTS $HUGE_PORT"; fi
# shellcheck disable=SC2086
for port in $READY_PORTS; do
  while ! curl -sf "http://localhost:$port/api/session" >/dev/null 2>&1; do
    sleep 0.1
  done
done

# Run tests
if [ $# -eq 0 ]; then
  # No args: run all projects in parallel (mobile after git-mode; see below)
  PWLOGS=$(mktemp -d)
  FAILED=0

  # Record each project's real exit code. Playwright exits 0 when a test only
  # flaked and passed on retry, so the log text alone can't tell a recovered
  # flake from a hard failure — both print "failed" in the error detail.
  reap() { # name pid
    local rc=0
    wait "$2" || rc=$?
    echo "$rc" > "$PWLOGS/$1.rc"
    [ "$rc" -eq 0 ] || FAILED=1
  }

  # E2E_ONLY_SERVER (see playwright.config.ts) keeps each invocation's
  # managed webServers to the one fixture it needs. Without it, every
  # parallel invocation would race to boot the entries run.sh skipped.
  if [ "$START_GIT" = 1 ]; then
    E2E_ONLY_SERVER=git npx playwright test --project=git-mode --shard=1/2 > "$PWLOGS/git-1.log" 2>&1 &
    PW_GIT1=$!
  fi
  if [ "$START_GIT2" = 1 ]; then
    CRIT_TEST_PORT="$GIT2_PORT" E2E_ONLY_SERVER=git npx playwright test --project=git-mode --shard=2/2 > "$PWLOGS/git-2.log" 2>&1 &
    PW_GIT2=$!
  fi
  if [ "$START_FILE" = 1 ]; then
    E2E_ONLY_SERVER=file npx playwright test --project=file-mode > "$PWLOGS/file.log" 2>&1 &
    PW_FILE=$!
  fi
  if [ "$START_SINGLE" = 1 ]; then
    E2E_ONLY_SERVER=single npx playwright test --project=single-file-mode > "$PWLOGS/single.log" 2>&1 &
    PW_SINGLE=$!
  fi
  if [ "$START_NOGIT" = 1 ]; then
    E2E_ONLY_SERVER=nogit npx playwright test --project=no-git-mode > "$PWLOGS/nogit.log" 2>&1 &
    PW_NOGIT=$!
  fi
  if [ "$START_MULTI" = 1 ]; then
    E2E_ONLY_SERVER=multi npx playwright test --project=multi-file-mode > "$PWLOGS/multi.log" 2>&1 &
    PW_MULTI=$!
  fi
  if [ "$START_RANGE" = 1 ]; then
    E2E_ONLY_SERVER=range npx playwright test --project=range-mode > "$PWLOGS/range.log" 2>&1 &
    PW_RANGE=$!
  fi
  if [ "$START_LIVE" = 1 ]; then
    E2E_ONLY_SERVER=live npx playwright test --project=live-mode > "$PWLOGS/live.log" 2>&1 &
    PW_LIVE=$!
  fi
  if [ "$START_SHARE" = 1 ]; then
    E2E_ONLY_SERVER=share npx playwright test --project=share-transport > "$PWLOGS/share.log" 2>&1 &
    PW_SHARE=$!
  fi
  if [ "$START_PERF" = 1 ]; then
    E2E_ONLY_SERVER=perf npx playwright test --project=perf > "$PWLOGS/perf.log" 2>&1 &
    PW_PERF=$!
  fi
  if [ "$START_HUGE" = 1 ]; then
    E2E_ONLY_SERVER=huge npx playwright test --project=huge > "$PWLOGS/huge.log" 2>&1 &
    PW_HUGE=$!
  fi

  # Mobile shares the git-mode fixture (port 3123) and both projects call
  # DELETE /api/comments in beforeEach, so they must not overlap. Wait for
  # both git-mode shards, then launch mobile against the first fixture.
  # Default path only: matrix shards don't run mobile (and Windows skips it
  # anyway — see below).
  # Skip on Windows — touch emulation is a Chromium feature identical across
  # OS, and Windows headless has reliability issues with touchscreen.tap().
  if [ "$START_GIT" = 1 ]; then reap git-1 $PW_GIT1; fi
  if [ "$START_GIT2" = 1 ]; then reap git-2 $PW_GIT2; fi
  if [ "$SHARD" = "all" ] && [[ "$OSTYPE" != msys && "$OSTYPE" != cygwin ]]; then
    E2E_ONLY_SERVER=git npx playwright test --project=mobile > "$PWLOGS/mobile.log" 2>&1 &
    PW_MOBILE=$!
  fi

  # Now wait for everything else.
  if [ "$START_FILE" = 1 ]; then reap file $PW_FILE; fi
  if [ "$START_SINGLE" = 1 ]; then reap single $PW_SINGLE; fi
  if [ "$START_NOGIT" = 1 ]; then reap nogit $PW_NOGIT; fi
  if [ "$START_MULTI" = 1 ]; then reap multi $PW_MULTI; fi
  if [ "$START_RANGE" = 1 ]; then reap range $PW_RANGE; fi
  if [ "$START_LIVE" = 1 ]; then reap live $PW_LIVE; fi
  if [ "$START_SHARE" = 1 ]; then reap share $PW_SHARE; fi
  if [ "$START_PERF" = 1 ]; then reap perf $PW_PERF; fi
  if [ "$START_HUGE" = 1 ]; then reap huge $PW_HUGE; fi
  if [ -n "${PW_MOBILE:-}" ]; then
    reap mobile $PW_MOBILE
  fi

  # Print results — show summary for passing projects, full output for failures
  for f in "$PWLOGS"/*.log; do
    name=$(basename "$f" .log)
    rc=$(cat "$PWLOGS/$name.rc" 2>/dev/null || echo 0)
    if [ "$rc" -ne 0 ]; then
      echo "=== $name (FAILED) ==="
      # Dump the full project log on failure so CI shows every error message
      # (a 30-line tail buries per-test errors when many tests fail).
      cat "$f"
    elif grep -q "flaky" "$f"; then
      echo "=== $name (passed, flaky on first attempt) ==="
      tail -5 "$f"
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
