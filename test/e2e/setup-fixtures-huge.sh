#!/usr/bin/env bash
# Huge-review fixture: ~2,500 added files (~225k changed lines), tall enough
# that the flat review document exceeds 4,194,304px (2^22). Chrome stops
# painting and hit-testing below that line under some CSS (single-axis
# overflow clip on a tall ancestor); *.huge.spec.ts guards deep navigation.
#
# Serves git mode on the given port for the huge project.
set -euo pipefail

PORT="${1:-3136}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CRIT_SRC="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
DIR=$(e2e_native_tempdir)
BIN_DIR=$(e2e_native_tempdir)
trap 'rm -rf "$DIR" "$BIN_DIR" "${FAKE_HOME:-}"' EXIT

FILES=2500
LINES_PER_FILE=90

cd "$DIR"
git init -q
git config user.email "test@test.com"
git config user.name "Test"
git config core.autocrlf false
git config core.eol lf

echo "# Huge review fixture" > README.md
git add -A
git commit -q -m "initial commit"

git checkout -q -b huge/review

# One awk process writes every file (a shell loop of 2.5k heredocs is slow on
# Git Bash). Nested dirs keep the file tree realistic.
awk -v files="$FILES" -v lines="$LINES_PER_FILE" 'BEGIN {
  for (i = 1; i <= files; i++) {
    dir = sprintf("pkg%02d", i % 40)
    system("mkdir -p " dir)
    path = sprintf("%s/file%04d.go", dir, i)
    printf("package pkg%d\n\n", i) > path
    for (l = 1; l <= lines; l++) {
      printf("func F%d_%d() int { return %d }\n", i, l, l) > path
    }
    close(path)
  }
}'

git add -A
git commit -q -m "huge: add ${FILES} files"

# Build crit binary outside the repo (skip if CRIT_BIN is set)
if [ -z "${CRIT_BIN:-}" ]; then
  CRIT_BIN="$BIN_DIR/$(e2e_bin_name)"
  (cd "$CRIT_SRC" && go build -o "$CRIT_BIN" ./cmd/crit)
fi

FAKE_HOME=$(e2e_native_tempdir)
e2e_export_fake_home "$FAKE_HOME"

STATE_FILE="$(e2e_state_file "$PORT")"
{
  echo "CRIT_BIN=$CRIT_BIN"
  echo "CRIT_FIXTURE_DIR=$DIR"
  echo "FAKE_HOME=$FAKE_HOME"
} > "$STATE_FILE"

echo '{"agent_cmd": "echo"}' > "$FAKE_HOME/.crit.config.json"

exec "$CRIT_BIN" _serve --no-open --port "$PORT"
