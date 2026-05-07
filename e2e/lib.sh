#!/usr/bin/env bash
# Shared helpers for e2e fixture scripts. Sourced from setup-fixtures*.sh and run.sh.
# Goal: keep one bash code path that works under Linux, macOS, and Git Bash on Windows.

# Detect Git Bash / MSYS / Cygwin environments.
case "${OSTYPE:-$(uname -s)}" in
  msys*|cygwin*|MINGW*|MSYS*) E2E_IS_WINDOWS=1 ;;
  *) E2E_IS_WINDOWS=0 ;;
esac
export E2E_IS_WINDOWS

# Directory where setup scripts write per-port state files that are read back
# by Playwright tests (Node side). MUST be a path that resolves identically
# for both Git Bash and Node on Windows — so we keep it inside the e2e/ dir.
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

# Kill anything listening on the given TCP port. Uses lsof on POSIX and
# powershell + taskkill on Windows (lsof isn't on Git Bash by default).
e2e_kill_port() {
  local port="$1"
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    # NOTE: we intentionally do not redirect stderr to /dev/null on the
    # taskkill line so that real errors (other than "process not found")
    # surface in CI logs.
    local pids
    pids=$(powershell.exe -NoProfile -Command \
      "Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess" \
      2>/dev/null | tr -d '\r' | sort -u | grep -E '^[0-9]+$' || true)
    if [ -n "$pids" ]; then
      for pid in $pids; do
        taskkill.exe /F /PID "$pid" /T >/dev/null 2>&1 || true
      done
    fi
  else
    lsof -ti tcp:"$port" 2>/dev/null | xargs kill -9 2>/dev/null || true
  fi
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
}

# Kill all stray crit processes — used by run.sh cleanup on Windows where
# `kill $PID` only kills the bash subshell, not the spawned crit.exe.
e2e_kill_stray_crit() {
  if [ "$E2E_IS_WINDOWS" -eq 1 ]; then
    taskkill.exe /F /IM crit.exe /T >/dev/null 2>&1 || true
  fi
}

# Ensure the state dir exists before any fixture writes to it.
mkdir -p "$(e2e_state_dir)" 2>/dev/null || true
