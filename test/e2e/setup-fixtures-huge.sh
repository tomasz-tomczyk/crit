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
e2e_cleanup_install
e2e_cleanup_add "$DIR" "$BIN_DIR"

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
# Git Bash). Nested dirs keep the file tree realistic. Create the 40 dirs up
# front: an awk system("mkdir") per file is 5,000 process spawns (~7s on
# Linux, minutes on Git Bash).
DIRS=40
for ((d = 0; d < DIRS; d++)); do printf -v dir 'pkg%02d' "$d"; mkdir -p "$dir"; done
awk -v files="$FILES" -v lines="$LINES_PER_FILE" -v dirs="$DIRS" 'BEGIN {
  for (i = 1; i <= files; i++) {
    path = sprintf("pkg%02d/file%04d.go", i % dirs, i)
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
e2e_cleanup_add "$FAKE_HOME"
e2e_export_fake_home "$FAKE_HOME"

STATE_FILE="$(e2e_state_file "$PORT")"
{
  echo "CRIT_BIN=$CRIT_BIN"
  echo "CRIT_FIXTURE_DIR=$DIR"
  echo "FAKE_HOME=$FAKE_HOME"
} > "$STATE_FILE"

echo '{"agent_cmd": "echo"}' > "$FAKE_HOME/.crit.config.json"

e2e_serve "$CRIT_BIN" _serve --no-open --port "$PORT"
