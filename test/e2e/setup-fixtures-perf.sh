#!/usr/bin/env bash
# Perf fixture: a 300-file review (~9k changed lines) plus one large markdown
# file, mirroring the shape measured in the lazy-loading fix (27f33c8:
# 300 files / 9,000 changed lines, 999k DOM nodes before the fix).
#
# Serves git mode on the given port for *.perf.spec.ts (perf project).
# Files beyond lazyFileThreshold (25) stay lazy until scrolled near.
set -euo pipefail

PORT="${1:-3134}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
DIR=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
e2e_cleanup_install
e2e_cleanup_add "$DIR" "$BIN_DIR"

cd "$DIR"
git init -q
git config user.email "test@test.com"
git config user.name "Test"
git config core.autocrlf false
git config core.eol lf

# === Initial commit: 300 small code files + one markdown file ===
# printf is a builtin: a `cat` heredoc per file is 600 process spawns, which
# Git Bash on Windows turns into ~20s of fixture boot.
for ((i = 1; i <= 300; i++)); do
  printf 'package perf%d\n\n// Doc comment for file %d.\nfunc Hello%d() string {\n\treturn "hello %d"\n}\n\nfunc Add%d(a, b int) int {\n\treturn a + b\n}\n' \
    "$i" "$i" "$i" "$i" "$i" > "file$i.go"
done

# Large markdown, initial version (~1200 lines).
{
  echo "# Perf Plan"
  echo ""
  for ((i = 1; i <= 200; i++)); do
    echo "## Section $i"
    echo ""
    echo "This section describes part $i of the plan with some detail."
    echo ""
    echo "- item one for $i"
    echo "- item two for $i"
    echo ""
  done
} > plan-big.md

git add -A
git commit -q -m "initial commit"

# === Feature branch: touch every file ===
git checkout -q -b perf/large-review

for ((i = 1; i <= 300; i++)); do
  printf '\n// Goodbye%d is new on the branch.\nfunc Goodbye%d() string {\n\treturn "goodbye %d"\n}\n' \
    "$i" "$i" "$i" >> "file$i.go"
done

# Grow the markdown to ~3000 lines with mixed block types.
{
  echo "# Perf Plan"
  echo ""
  for ((i = 1; i <= 400; i++)); do
    echo "## Section $i"
    echo ""
    echo "This section describes part $i of the plan with some detail."
    echo ""
    echo "- item one for $i"
    echo "  - nested item for $i"
    echo "- item two for $i"
    echo ""
    echo '```go'
    echo "func Section$i() {}"
    echo '```'
    echo ""
  done
} > plan-big.md

git add -A
git commit -q -m "perf: large review fixture"

# Build crit binary outside the repo (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

FAKE_HOME=$(e2e_native_tempdir)
e2e_cleanup_add "$FAKE_HOME"
e2e_export_fake_home "$FAKE_HOME"

STATE_FILE="$(e2e_state_file "$PORT")"
{
  echo "CRIT_BIN=$CRIT_BIN"
  echo "CRIT_FIXTURE_DIR=$DIR"
  echo "FAKE_HOME=$FAKE_HOME"
} > "$STATE_FILE"

echo '{"agent_cmd": "echo"}' > "$FAKE_HOME/.crit.config.json"

# Run crit in the fixture repo (git mode: branch diff vs default branch)
e2e_serve "$CRIT_BIN" _serve --no-open --port "$PORT"
