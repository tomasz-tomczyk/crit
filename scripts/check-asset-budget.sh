#!/usr/bin/env bash
# check-asset-budget.sh — fail CI when embedded assets or the binary blow past budget.
#
# crit ships every file in web/ inside the Go binary (embed.FS), so a heavy
# vendor bump or an accidentally committed fixture inflates both page weight
# and binary size. This script checks gzip sizes from asset-budget.json
# plus a linux/amd64 binary build. No bundler involved — plain file sizes.
#
# Usage: bash scripts/check-asset-budget.sh [--binary-only|--web-only]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUDGET="$ROOT/asset-budget.json"
MODE="${1:-all}"
FAIL=0

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing required tool: $1" >&2; exit 2; }; }
need node
need gzip

get() { node -e "const b=require('$BUDGET'); process.stdout.write(String($1));"; }

check_file() {
  local rel="$1" cap="$2"
  local f="$ROOT/$rel"
  if [ ! -f "$f" ]; then echo "MISS  $rel (not found)"; FAIL=1; return; fi
  local size
  size=$(gzip -9 -c "$f" | wc -c | tr -d ' ')
  if [ "$size" -gt "$cap" ]; then
    echo "OVER  $rel gzip=${size}B cap=${cap}B (+$((size - cap))B)"
    FAIL=1
  else
    echo "ok    $rel gzip=${size}B cap=${cap}B"
  fi
}

if [ "$MODE" = "all" ] || [ "$MODE" = "--web-only" ]; then
  echo "== web assets (gzip) =="
  while IFS= read -r row; do
    check_file "$(echo "$row" | cut -d' ' -f1)" "$(echo "$row" | cut -d' ' -f2)"
  done < <(node -e "const b=require('$BUDGET'); for (const f of b.files) console.log(f.path+' '+f.gzipBytes);")

  total_cap=$(get 'b.totalJsGzipBytes')
  total=$(cat "$ROOT"/web/*.js | gzip -9 -c | wc -c | tr -d ' ')
  if [ "$total" -gt "$total_cap" ]; then
    echo "OVER  web/*.js (total) gzip=${total}B cap=${total_cap}B (+$((total - total_cap))B)"
    FAIL=1
  else
    echo "ok    web/*.js (total) gzip=${total}B cap=${total_cap}B"
  fi

  echo "== module globs (gzip) =="
  while IFS= read -r row; do
    pattern=$(echo "$row" | cut -d' ' -f1)
    cap=$(echo "$row" | cut -d' ' -f2)
    # shellcheck disable=SC2086 — glob must expand unquoted by design
    size=$(cat $ROOT/$pattern | gzip -9 -c | wc -c | tr -d ' ')
    if [ "$size" -gt "$cap" ]; then
      echo "OVER  $pattern (total) gzip=${size}B cap=${cap}B (+$((size - cap))B)"
      FAIL=1
    else
      echo "ok    $pattern (total) gzip=${size}B cap=${cap}B"
    fi
  done < <(node -e "const b=require('$BUDGET'); for (const g of (b.globs || [])) console.log(g.pattern+' '+g.gzipBytes);")
fi

if [ "$MODE" = "all" ] || [ "$MODE" = "--binary-only" ]; then
  echo "== binary =="
  need go
  bin_cap=$(get 'b.binaryBytes')
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  out="$tmpdir/crit-sizeprobe"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$out" ./cmd/crit
  size=$(wc -c < "$out" | tr -d ' ')
  rm -rf "$tmpdir"
  trap - EXIT
  if [ "$size" -gt "$bin_cap" ]; then
    echo "OVER  crit binary (linux/amd64) ${size}B cap=${bin_cap}B (+$((size - bin_cap))B)"
    FAIL=1
  else
    echo "ok    crit binary (linux/amd64) ${size}B cap=${bin_cap}B"
  fi
fi

if [ "$FAIL" -ne 0 ]; then
  echo "asset budget exceeded — if the growth is intentional, update caps in asset-budget.json" >&2
  exit 1
fi
echo "asset budget: all within caps"
