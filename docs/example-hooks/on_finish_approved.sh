#!/usr/bin/env sh
# Example on_finish_approved hook — append an audit line per approved review.
#
# Crit exposes the review context as env vars (CRIT_*) and a JSON payload on
# stdin. See docs/agent-hooks.md for the full reference. Runs with $PWD = repo
# root. Edit freely — this file is an example, not a default behavior.

: "${CRIT_REVIEW_PATH:?missing CRIT_REVIEW_PATH}"
: "${CRIT_SESSION_KEY:=unknown}"
: "${CRIT_MODE:=}"

log="$HOME/.crit/reviews/approved.log"
mkdir -p "$(dirname "$log")"

ts=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date 2>/dev/null || echo "?")
printf '%s\t%s\t%s\t%s\n' "$ts" "$CRIT_SESSION_KEY" "$CRIT_MODE" "$CRIT_REVIEW_PATH" >>"$log"

echo "crit: logged approval to $log" >&2