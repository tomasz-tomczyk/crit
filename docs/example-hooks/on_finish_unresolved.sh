#!/usr/bin/env sh
# Example on_finish_unresolved hook — snapshot commented-on files.
#
# Build up a persistent dataset of code patterns the agent handled poorly by
# copying the files you commented on alongside the review JSON.
#
# Crit exposes the review context as env vars (CRIT_*) and a JSON payload on
# stdin. See docs/agent-hooks.md for the full reference.
#
# Runs with $PWD = the repo root, so $CRIT_FILES_WITH_COMMENTS (repo-relative,
# newline-separated) can be copied directly. Existing snapshots are not
# overwritten — each session gets its own directory keyed by $CRIT_SESSION_KEY.

set -eu

: "${CRIT_REVIEW_PATH:?missing CRIT_REVIEW_PATH}"
: "${CRIT_SESSION_KEY:?missing CRIT_SESSION_KEY}"
: "${CRIT_UNRESOLVED_COUNT:=0}"

if [ "$CRIT_UNRESOLVED_COUNT" -eq 0 ]; then
  exit 0
fi

dest="$HOME/.crit/reviews/references/$CRIT_SESSION_KEY"
mkdir -p "$dest"

# Persist the review JSON itself (comments + quotes + anchors).
cp "$CRIT_REVIEW_PATH" "$dest/review.json"

# Snapshot each commented-on file. File-level / review-level comments don't
# list a file, so fall back to every file in $CRIT_FILES_WITH_COMMENTS — when
# empty, jq (if available) can derive paths from the stdin JSON payload.
if [ -n "${CRIT_FILES_WITH_COMMENTS:-}" ]; then
  printf '%s\n' "$CRIT_FILES_WITH_COMMENTS" | while IFS= read -r f; do
    [ -n "$f" ] || continue
    [ -f "$f" ] || continue
    mkdir -p "$dest/files/$(dirname "$f")"
    cp "$f" "$dest/files/$f"
  done
else
  if command -v jq >/dev/null 2>&1; then
    jq -r '.files_with_comments[]?' | while IFS= read -r f; do
      [ -n "$f" ] || continue
      [ -f "$f" ] || continue
      mkdir -p "$dest/files/$(dirname "$f")"
      cp "$f" "$dest/files/$f"
    done
  fi
fi

echo "crit: snapshotted $CRIT_UNRESOLVED_COUNT unresolved comment(s) to $dest" >&2