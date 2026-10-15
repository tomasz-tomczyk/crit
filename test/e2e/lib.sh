#!/usr/bin/env bash
# Shared helpers for e2e fixture scripts. Sourced from setup-fixtures*.sh and run.sh.
# Goal: keep one bash code path that works under Linux, macOS, and Git Bash on Windows.

# Detect Git Bash / MSYS / Cygwin environments.
case "${OSTYPE:-$(uname -s)}" in
  msys*|cygwin*|MINGW*|MSYS*) E2E_IS_WINDOWS=1 ;;
  *) E2E_IS_WINDOWS=0 ;;
esac
export E2E_IS_WINDOWS

# No automatic git maintenance in fixture repos. Newer git (2.5x) runs
# `git maintenance run --auto --detach` after a commit that leaves many loose
# objects, so the next `git add` in a fixture script races a background
# repack that deletes .git/objects/XX dirs ("unable to create temporary
# file"). Applies to every git the fixture scripts and crit run.
# Append after any GIT_CONFIG_* entries already in the environment, and only
# once: run.sh sources this file and so does every fixture script it starts.
if [ -z "${E2E_GIT_MAINTENANCE_OFF:-}" ]; then
  _e2e_n="${GIT_CONFIG_COUNT:-0}"
  export "GIT_CONFIG_KEY_$_e2e_n=maintenance.auto" "GIT_CONFIG_VALUE_$_e2e_n=false"
  _e2e_n=$((_e2e_n + 1))
  export "GIT_CONFIG_KEY_$_e2e_n=gc.auto" "GIT_CONFIG_VALUE_$_e2e_n=0"
  export GIT_CONFIG_COUNT=$((_e2e_n + 1)) E2E_GIT_MAINTENANCE_OFF=1
  unset _e2e_n
fi

# Directory where setup scripts write per-port state files that are read back
# by Playwright tests (Node side). MUST be a path that resolves identically
# for both Git Bash and Node on Windows — so we keep it inside the test/e2e/ dir.
# Override with CRIT_E2E_STATE_DIR if you need a different location.
# Capture lib.sh's directory at source time. BASH_SOURCE inside a function
# can resolve to the caller in some shells/versions, so we freeze it here.
_E2E_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

e2e_state_dir() {
  if [ -n "${CRIT_E2E_STATE_DIR:-}" ]; then
    printf '%s' "$CRIT_E2E_STATE_DIR"
    return
  fi
  printf '%s/.state' "$_E2E_LIB_DIR"
}

e2e_state_file() {
  local port="$1"
  printf '%s/crit-e2e-state-%s' "$(e2e_state_dir)" "$port"
}

# Convert a (possibly MSYS-style) path to native Windows form on Git Bash,
# leaving it unchanged on POSIX. Use this on every fixture tempdir so that
# Go's filepath.Join (running under Windows) and Node's child_process cwd
# get a path with a real drive letter.
#
# `realpath` on Git Bash returns POSIX form (e.g. /tmp/tmp.XXX). Go's
# filepath.Join on Windows then builds `\tmp\tmp.XXX\.crit\reviews\...`,
# which lacks a drive letter — Windows resolves it relative to the calling
# process's current drive, so the daemon writing on D: and a Node test
# reading on C: see different files. Node spawn(cwd) on a POSIX path also
# fails outright with ENOENT before launching the shell.
#
# `cygpath -m` produces mixed form (C:/Users/...) which Go and Node both
# understand. Fall back to the input unchanged if cygpath isn't available.
e2e_native_path() {
  local p="$1"
  if [ "$E2E_IS_WINDOWS" -eq 1 ] && command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$p"
  else
    printf '%s' "$p"
  fi
}

# Make a fresh tempdir and emit it in native form (drive-letter-prefixed on
# Windows). Mirrors `realpath "$(mktemp -d)"` but produces a path that Go
# and Node interpret consistently.
e2e_native_tempdir() {
  e2e_native_path "$(mktemp -d)"
}

# On Windows `go build -o foo` produces foo.exe automatically. Tests that
# spawn the binary from Node need the .exe suffix in CRIT_BIN.
e2e_bin_name() {
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    printf 'crit.exe'
  else
    printf 'crit'
  fi
}

# Kill anything listening on the given TCP port(s). Uses lsof on POSIX and a
# single powershell + taskkill pass on Windows (lsof isn't on Git Bash by
# default). PowerShell cold start costs ~1s on CI, so callers should pass every
# port in one call rather than looping.
e2e_kill_ports() {
  [ "$#" -gt 0 ] || return 0
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    # NOTE: we intentionally do not redirect stderr to /dev/null on the
    # taskkill line so that real errors (other than "process not found")
    # surface in CI logs.
    local port_list pids pid
    port_list=$(IFS=,; printf '%s' "$*")
    pids=$(powershell.exe -NoProfile -Command \
      "Get-NetTCPConnection -LocalPort $port_list -State Listen -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess" \
      2>/dev/null | tr -d '\r' | sort -u | grep -E '^[0-9]+$' || true)
    for pid in $pids; do
      taskkill.exe /F /PID "$pid" /T >/dev/null 2>&1 || true
    done
  else
    local port
    for port in "$@"; do
      lsof -ti tcp:"$port" 2>/dev/null | xargs kill -9 2>/dev/null || true
    done
  fi
}

e2e_kill_port() {
  e2e_kill_ports "$1"
}

# True when a Chromium headless shell is in Playwright's browser cache
# (PLAYWRIGHT_BROWSERS_PATH, else the per-OS default). Version-agnostic: CI
# keys that cache on package-lock.json, so a hit is the pinned build.
e2e_playwright_browser_present() {
  local dir="${PLAYWRIGHT_BROWSERS_PATH:-}" d
  if [ -z "$dir" ]; then
    if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
      dir="$(e2e_native_path "${LOCALAPPDATA:-$HOME/AppData/Local}")/ms-playwright"
    elif [ "$(uname -s)" = Darwin ]; then
      dir="$HOME/Library/Caches/ms-playwright"
    else
      dir="${XDG_CACHE_HOME:-$HOME/.cache}/ms-playwright"
    fi
  fi
  for d in "$dir"/chromium_headless_shell-*; do
    [ -d "$d" ] && return 0
  done
  return 1
}

# Block until the fixture on $port answers /api/session.
#   e2e_wait_port <port> <fixture-pid> [deadline-seconds]
# Fails (returns 1, with a message on stderr) if the fixture process exits
# first or the deadline passes, instead of polling forever.
e2e_wait_port() {
  local port="$1" pid="$2" deadline="${3:-${E2E_READY_TIMEOUT:-300}}"
  local start=$SECONDS
  while ! curl -sf "http://127.0.0.1:$port/api/session" >/dev/null 2>&1; do
    if [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null; then
      echo "e2e: fixture for port $port (pid $pid) exited before becoming ready" >&2
      return 1
    fi
    if [ $((SECONDS - start)) -ge "$deadline" ]; then
      echo "e2e: fixture for port $port not ready after ${deadline}s" >&2
      return 1
    fi
    sleep 0.1
  done
}

# Set HOME (and USERPROFILE on Windows) so crit picks up our isolated config.
# Go's os.UserHomeDir on Windows uses USERPROFILE, not HOME. Callers MUST
# pass a path produced by e2e_native_path / e2e_native_tempdir so that
# USERPROFILE is a drive-letter-prefixed path (e.g. C:/Users/...) — Go's
# filepath.Join on Windows does not understand MSYS-style /tmp/... paths
# and silently builds `\tmp\...` which resolves against the calling
# process's current drive (so the daemon and Node tests can disagree).
e2e_export_fake_home() {
  local fake_home="$1"
  export HOME="$fake_home"
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    export USERPROFILE="$fake_home"
  fi
  # Skip the daemon's agent-CLI `--version` probes: on machines with agent
  # CLIs on PATH they block session setup for several seconds, and no e2e
  # test exercises the integration check.
  export CRIT_NO_INTEGRATION_CHECK=1
  export CRIT_NO_UPDATE_CHECK=1
}

# Kill all stray fixture processes — used by run.sh cleanup on Windows where
# `kill $PID` only kills the bash subshell, not the spawned native children:
# crit.exe, plus the live-mode upstream and share-transport stub binaries
# (the upstream binds port 0, so a later e2e_kill_ports never finds it).
e2e_kill_stray_crit() {
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    taskkill.exe /F /IM crit.exe /T >/dev/null 2>&1 || true
    taskkill.exe /F /IM upstream.exe /T >/dev/null 2>&1 || true
    taskkill.exe /F /IM stub-crit-web.exe /T >/dev/null 2>&1 || true
  fi
}

# ---- Fixture lifecycle -------------------------------------------------------
# Fixture scripts register temp paths with e2e_cleanup_add and start their
# long-running children through e2e_spawn / e2e_serve. e2e_cleanup_install
# arms one trap for EXIT, INT and TERM: run.sh and Playwright stop fixtures
# with a signal while the script is `wait`ing, and a bare EXIT trap (or an
# `exec crit`) never ran on that path, leaking every temp dir. The trap kills
# and reaps the children before rm -rf so the delete doesn't race a process
# still holding files open (Windows: "Device or resource busy").
E2E_CLEANUP_PATHS=()
E2E_CHILD_PIDS=()

e2e_cleanup_add() {
  E2E_CLEANUP_PATHS+=("$@")
}

e2e_cleanup() {
  local pid
  trap - EXIT INT TERM
  for pid in "${E2E_CHILD_PIDS[@]+"${E2E_CHILD_PIDS[@]}"}"; do
    kill "$pid" 2>/dev/null || true
  done
  for pid in "${E2E_CHILD_PIDS[@]+"${E2E_CHILD_PIDS[@]}"}"; do
    wait "$pid" 2>/dev/null || true
  done
  # Leave the fixture dir first: Windows refuses to delete a process's cwd.
  cd / 2>/dev/null || true
  if [ "${#E2E_CLEANUP_PATHS[@]}" -gt 0 ]; then
    rm -rf "${E2E_CLEANUP_PATHS[@]}" 2>/dev/null || true
  fi
}

e2e_cleanup_install() {
  trap e2e_cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

# Start a background child the cleanup trap must stop. Sets E2E_LAST_PID.
e2e_spawn() {
  "$@" &
  E2E_LAST_PID=$!
  E2E_CHILD_PIDS+=("$E2E_LAST_PID")
}

# Run the fixture's crit server in the background and wait on it, so the
# script stays alive to clean up when it is signalled. Replaces `exec crit`.
e2e_serve() {
  e2e_spawn "$@"
  wait "$E2E_LAST_PID"
}

# Ensure the state dir exists before any fixture writes to it.
mkdir -p "$(e2e_state_dir)" 2>/dev/null || true
