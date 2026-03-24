#!/usr/bin/env bash
set -euo pipefail

log_file=$(mktemp)
trap 'rm -f "$log_file"' EXIT

set +e
nix build . 2>&1 | tee "$log_file"
status=${PIPESTATUS[0]}
set -e

if [ "$status" -ne 0 ]; then
  hash=$(grep -oE 'got:[[:space:]]*sha256-[A-Za-z0-9+/=]+' "$log_file" | sed -E 's/^got:[[:space:]]*//' | tail -n1 || true)

  if [ -n "$hash" ]; then
    echo "Expected vendorHash: $hash"

    if [ -n "${GITHUB_ACTIONS:-}" ]; then
      echo "::error::Nix vendor hash mismatch. Update flake.nix to use $hash"
    fi

    if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
      {
        echo "### Nix vendor hash mismatch"
        echo
        echo 'Set `vendorHash` in `flake.nix` to:'
        echo
        echo '```nix'
        echo "$hash"
        echo '```'
      } >> "$GITHUB_STEP_SUMMARY"
    fi
  else
    message="Nix build failed. Could not extract a replacement vendor hash from the build log."
    echo "$message"

    if [ -n "${GITHUB_ACTIONS:-}" ]; then
      echo "::error::$message"
    fi
  fi

  exit "$status"
fi

nix run . -- --help
