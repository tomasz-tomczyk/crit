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
# first half + file/single/multi/huge; "2/2" runs the git second half +
# range/live/share/perf. Unset (default) runs everything: local runs and
# Linux CI are unchanged. Mobile runs in the default path only (it reuses
# the second git fixture once that git shard is done).
SHARD="${E2E_SHARD:-all}"
START_GIT=1; START_GIT2=1; START_FILE=1; START_SINGLE=1
START_MULTI=1; START_RANGE=1; START_LIVE=1; START_SHARE=1; START_PERF=1
START_HUGE=1
if [ "$SHARD" = "1/2" ]; then
  # Runs git --shard=1/2 against the GIT_PORT fixture, so GIT2 is unneeded.
  START_GIT2=0
  START_RANGE=0; START_LIVE=0; START_SHARE=0; START_PERF=0
elif [ "$SHARD" = "2/2" ]; then
  # Runs git --shard=2/2 against the GIT2_PORT fixture, so GIT is unneeded.
  START_GIT=0
  START_FILE=0; START_SINGLE=0; START_MULTI=0; START_HUGE=0
fi

# PIDs of fixture scripts and Playwright invocations. Per-port / per-project
# lookups use FIXTURE_PID_<port> / PW_PID_<name> variables via indirection
# (no associative arrays: macOS ships bash 3.2).
FIXTURE_PIDS=()
PW_PIDS=()
PW_NAMES=()

cleanup() {
  local pid
  trap - EXIT INT TERM
  for pid in "${PW_PIDS[@]+"${PW_PIDS[@]}"}"; do kill "$pid" 2>/dev/null || true; done
  # TERM lets each fixture script's trap stop its crit child and delete its
  # temp dirs (see e2e_cleanup in lib.sh); then reap them. Bash runs that
  # trap only after the current foreground command exits, so a fixture stuck
  # in setup (a hung git or go build, one that never got ready) would block
  # the wait below. Signal its direct children too.
  for pid in "${FIXTURE_PIDS[@]+"${FIXTURE_PIDS[@]}"}"; do
    kill "$pid" 2>/dev/null || true
    if command -v pkill >/dev/null 2>&1; then pkill -TERM -P "$pid" 2>/dev/null || true; fi
  done
  for pid in "${FIXTURE_PIDS[@]+"${FIXTURE_PIDS[@]}"}" "${PW_PIDS[@]+"${PW_PIDS[@]}"}"; do
    wait "$pid" 2>/dev/null || true
  done
  # On Git Bash `kill <bash-pid>` doesn't reap the spawned native children;
  # taskkill /T flushes crit.exe, upstream.exe and stub-crit-web.exe.
  e2e_kill_stray_crit
  rm -rf "${BIN_DIR:-}" "${PWLOGS:-}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Build crit once (skip if CRIT_BIN already points to an existing binary, e.g. CI coverage builds)
if [ -n "${CRIT_BIN:-}" ] && [ -f "$CRIT_BIN" ]; then
  echo "Using pre-built binary: $CRIT_BIN"
else
  BIN_DIR=$(mktemp -d)
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  export CRIT_BIN
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

# Ensure the Chromium build matching this project's pinned @playwright/test is
# installed. The browser cache is global and version-specific, so a machine
# that only has another project's Playwright version (e.g. crit-web's) won't
# have the build crit needs and every test fails with "Executable doesn't
# exist". CI installs (or restores) the browser in an earlier workflow step
# keyed on package-lock.json, so skip the npx round trip there when the
# headless shell is already on disk.
if [ -n "${CI:-}" ] && e2e_playwright_browser_present; then
  echo "Playwright Chromium already installed; skipping install"
else
  (cd "$SCRIPT_DIR" && npx playwright install chromium)
fi

# Kill any stale processes on our test ports before starting fresh (one
# PowerShell call on Windows, not one per port).
e2e_kill_ports "$GIT_PORT" "$GIT2_PORT" "$FILE_PORT" "$SINGLE_PORT" "$MULTI_PORT" \
  "$RANGE_PORT" "$LIVE_PORT" "$SHARE_PORT" "$STUB_PORT" "$STUB2_PORT" "$PERF_PORT" "$HUGE_PORT"

# Start fixture servers in parallel — only the ones this shard's projects
# need, so a matrix shard doesn't pay for (or contend with) idle servers.
cd "$SCRIPT_DIR"
start_fixture() { # port script [args...]
  local port="$1"; shift
  bash "$@" &
  printf -v "FIXTURE_PID_$port" '%s' "$!"
  FIXTURE_PIDS+=("$!")
  STARTED_PORTS+=("$port")
}
STARTED_PORTS=()
if [ "$START_GIT" = 1 ]; then start_fixture "$GIT_PORT" setup-fixtures.sh "$GIT_PORT"; fi
if [ "$START_GIT2" = 1 ]; then start_fixture "$GIT2_PORT" setup-fixtures.sh "$GIT2_PORT"; fi
if [ "$START_FILE" = 1 ]; then start_fixture "$FILE_PORT" setup-fixtures-filemode.sh "$FILE_PORT"; fi
if [ "$START_SINGLE" = 1 ]; then start_fixture "$SINGLE_PORT" setup-fixtures-singlefile.sh "$SINGLE_PORT"; fi
if [ "$START_MULTI" = 1 ]; then start_fixture "$MULTI_PORT" setup-fixtures-multifile.sh "$MULTI_PORT"; fi
if [ "$START_RANGE" = 1 ]; then start_fixture "$RANGE_PORT" setup-fixtures-range-mode.sh "$RANGE_PORT"; fi
if [ "$START_LIVE" = 1 ]; then start_fixture "$LIVE_PORT" setup-fixtures-livemode.sh "$LIVE_PORT"; fi
if [ "$START_SHARE" = 1 ]; then start_fixture "$SHARE_PORT" setup-fixtures-sharetransport.sh "$SHARE_PORT" "$STUB_PORT" "$STUB2_PORT"; fi
if [ "$START_PERF" = 1 ]; then start_fixture "$PERF_PORT" setup-fixtures-perf.sh "$PERF_PORT"; fi
if [ "$START_HUGE" = 1 ]; then start_fixture "$HUGE_PORT" setup-fixtures-huge.sh "$HUGE_PORT"; fi

# Block until the fixture on $1 answers; fail fast if it died or hung.
wait_fixture() {
  local var="FIXTURE_PID_$1"
  e2e_wait_port "$1" "${!var}"
}

if [ $# -gt 0 ]; then
  # Custom args: wait for every started fixture, then run as-is.
  for port in ${STARTED_PORTS[@]+"${STARTED_PORTS[@]}"}; do wait_fixture "$port"; done
  npx playwright test "$@"
  exit $?
fi

# No args: run all projects in parallel. Each project starts as soon as its
# own fixture is ready, so a slow fixture (huge, perf) never delays git-mode.
PWLOGS=$(mktemp -d)
FAILED=0

# Each invocation gets its own --output dir under test-results/. Playwright
# empties its output dir when it starts, so with one shared dir a project that
# starts late (mobile, or one with a slow fixture) wiped the failure
# screenshots of projects that had already finished. Clear the parent once.
rm -rf test-results

# run_project <log-name> <port> [env assignments...] -- <playwright args...>
# E2E_ONLY_SERVER (see playwright.config.ts) keeps each invocation's managed
# webServers to the one fixture it needs; without it every parallel
# invocation would race to boot the entries run.sh skipped.
pw_var() { printf 'PW_PID_%s' "${1//-/_}"; }
run_project() {
  local name="$1" port="$2"; shift 2
  local envs=()
  while [ "$1" != "--" ]; do envs+=("$1"); shift; done
  shift
  (
    wait_fixture "$port" || exit 1
    exec env ${envs[@]+"${envs[@]}"} npx playwright test --output="test-results/$name" "$@"
  ) > "$PWLOGS/$name.log" 2>&1 &
  printf -v "$(pw_var "$name")" '%s' "$!"
  PW_PIDS+=("$!")
  PW_NAMES+=("$name")
}

# Record each project's real exit code. Playwright exits 0 when a test only
# flaked and passed on retry, so the log text alone can't tell a recovered
# flake from a hard failure — both print "failed" in the error detail.
reap() { # name
  local rc=0 var
  var=$(pw_var "$1")
  wait "${!var}" || rc=$?
  echo "$rc" > "$PWLOGS/$1.rc"
  [ "$rc" -eq 0 ] || FAILED=1
}

if [ "$START_GIT" = 1 ]; then run_project git-1 "$GIT_PORT" E2E_ONLY_SERVER=git -- --project=git-mode --shard=1/2; fi
if [ "$START_GIT2" = 1 ]; then run_project git-2 "$GIT2_PORT" CRIT_TEST_PORT="$GIT2_PORT" E2E_ONLY_SERVER=git -- --project=git-mode --shard=2/2; fi
if [ "$START_FILE" = 1 ]; then run_project file "$FILE_PORT" E2E_ONLY_SERVER=file -- --project=file-mode; fi
if [ "$START_SINGLE" = 1 ]; then run_project single "$SINGLE_PORT" E2E_ONLY_SERVER=single -- --project=single-file-mode; fi
if [ "$START_MULTI" = 1 ]; then run_project multi "$MULTI_PORT" E2E_ONLY_SERVER=multi -- --project=multi-file-mode; fi
if [ "$START_RANGE" = 1 ]; then run_project range "$RANGE_PORT" E2E_ONLY_SERVER=range -- --project=range-mode; fi
if [ "$START_LIVE" = 1 ]; then run_project live "$LIVE_PORT" E2E_ONLY_SERVER=live -- --project=live-mode; fi
if [ "$START_SHARE" = 1 ]; then run_project share "$SHARE_PORT" E2E_ONLY_SERVER=share -- --project=share-transport; fi
if [ "$START_PERF" = 1 ]; then run_project perf "$PERF_PORT" E2E_ONLY_SERVER=perf -- --project=perf; fi
if [ "$START_HUGE" = 1 ]; then run_project huge "$HUGE_PORT" E2E_ONLY_SERVER=huge -- --project=huge; fi

# Mobile reuses a git-mode fixture, and both projects DELETE /api/comments in
# beforeEach, so it must never overlap the git shard on that fixture. It runs
# on the GIT2 fixture as soon as git shard 2/2 (the shorter half) finishes,
# while git shard 1/2 is still running on its own fixture.
# Default path only: matrix shards don't run mobile. Skipped on Windows —
# touch emulation is a Chromium feature identical across OS, and Windows
# headless has reliability issues with touchscreen.tap().
if [ "$START_GIT2" = 1 ]; then reap git-2; fi
if [ "$SHARD" = "all" ] && [[ "$OSTYPE" != msys && "$OSTYPE" != cygwin ]]; then
  run_project mobile "$GIT2_PORT" CRIT_TEST_PORT="$GIT2_PORT" E2E_ONLY_SERVER=git -- --project=mobile
fi

# Now wait for everything else.
for name in ${PW_NAMES[@]+"${PW_NAMES[@]}"}; do
  [ "$name" = git-2 ] || reap "$name"
done

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

# Merge each project's .last-run.json into test-results/.last-run.json, the
# file `make e2e-failed` (--last-failed) reads.
node -e '
const fs = require("fs"), path = require("path");
const root = "test-results", failed = [];
for (const d of fs.readdirSync(root)) {
  try {
    const r = JSON.parse(fs.readFileSync(path.join(root, d, ".last-run.json"), "utf8"));
    failed.push(...(r.failedTests || []));
  } catch (e) {}
}
fs.writeFileSync(path.join(root, ".last-run.json"),
  JSON.stringify({ status: failed.length ? "failed" : "passed", failedTests: failed }, null, 2));
' 2>/dev/null || echo "e2e: could not merge .last-run.json files" >&2

if [ $FAILED -ne 0 ]; then
  echo "Some projects failed. Run 'make e2e-failed' or check individual project logs."
  exit 1
fi
