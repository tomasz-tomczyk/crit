#!/bin/bash
# Check that all var(--xxx) references resolve to defined CSS custom properties
set -e

# Variables set dynamically via JS or intentionally undefined (fallback value used)
DYNAMIC_VARS="--header-height --font-sans"

# Extract all var(--xxx) references (POSIX ERE, works on macOS and Linux)
REFS=$(grep -oE 'var\(--[a-zA-Z0-9_-]+' frontend/style.css frontend/theme.css 2>/dev/null | sed 's/.*var(//' | sort -u)

# Extract all --xxx: definitions (use perl for lookahead, portable)
DEFS=$(perl -nle 'print $1 if /^\s*(--[a-zA-Z0-9_-]+)\s*:/' frontend/theme.css frontend/style.css 2>/dev/null | sort -u)

# Add dynamic vars to definitions
for v in $DYNAMIC_VARS; do
    DEFS=$(printf '%s\n%s' "$DEFS" "$v")
done
DEFS=$(echo "$DEFS" | sort -u)

MISSING=$(comm -23 <(echo "$REFS") <(echo "$DEFS"))

if [ -n "$MISSING" ]; then
    echo "Undefined CSS variables:"
    echo "$MISSING"
    exit 1
fi

echo "All CSS variables are defined."
