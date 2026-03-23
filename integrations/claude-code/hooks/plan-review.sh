#!/usr/bin/env bash
# plan-review.sh — PermissionRequest hook for ExitPlanMode

LOG="/tmp/crit-plan-hook.log"

# Opt out: CRIT_PLAN_REVIEW=off disables crit review, falls through to normal CC behavior
if [ "${CRIT_PLAN_REVIEW:-}" = "off" ]; then
  exit 0
fi

echo "=== Hook fired at $(date) ===" >> "$LOG"

EVENT=$(cat)
echo "Event length: ${#EVENT}" >> "$LOG"

# Extract plan content
PLAN=""
if command -v jq &>/dev/null; then
  PLAN=$(echo "$EVENT" | jq -r '.tool_input.plan // empty' 2>>"$LOG")
elif command -v python3 &>/dev/null; then
  PLAN=$(echo "$EVENT" | python3 -c "import sys,json; e=json.load(sys.stdin); print(e.get('tool_input',{}).get('plan',''))" 2>>"$LOG")
else
  exit 0
fi

if [ -z "$PLAN" ]; then
  echo "No plan content, allowing through" >> "$LOG"
  exit 0
fi

echo "Plan length: ${#PLAN}" >> "$LOG"

# Find crit binary
CRIT=""
for candidate in "$HOME/.local/bin/crit" "/opt/homebrew/bin/crit" "/usr/local/bin/crit" crit; do
  if [ -x "$candidate" ] || command -v "$candidate" &>/dev/null; then
    CRIT="$candidate"
    break
  fi
done

if [ -z "$CRIT" ]; then
  echo "crit not found" >> "$LOG"
  exit 0
fi

echo "Using crit: $CRIT" >> "$LOG"
echo "Launching crit plan..." >> "$LOG"
RESULT=$(echo "$PLAN" | "$CRIT" plan 2>>"$LOG") || true
echo "crit result: ${RESULT:0:300}" >> "$LOG"

if [ -z "$RESULT" ]; then
  echo "Empty result, allowing through" >> "$LOG"
  exit 0
fi

APPROVED=$(echo "$RESULT" | jq -r '.approved // false' 2>/dev/null || echo "false")
echo "Approved: $APPROVED" >> "$LOG"

if [ "$APPROVED" = "true" ]; then
  OUTPUT='{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}'
  echo "Output: $OUTPUT" >> "$LOG"
  echo "$OUTPUT"
else
  PROMPT=$(echo "$RESULT" | jq -r '.prompt // "Review comments pending"' 2>/dev/null || echo "Review comments pending")
  OUTPUT=$(python3 -c "
import json, sys
prompt = sys.stdin.read()
resp = {
    'hookSpecificOutput': {
        'hookEventName': 'PermissionRequest',
        'decision': {'behavior': 'deny', 'message': prompt}
    }
}
print(json.dumps(resp))
" <<< "$PROMPT")
  echo "Output: ${OUTPUT:0:400}" >> "$LOG"
  echo "$OUTPUT"
fi

echo "=== Hook complete ===" >> "$LOG"
