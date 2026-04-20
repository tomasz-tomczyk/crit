#!/bin/bash
# Check that all var(--xxx) references resolve to defined CSS custom properties,
# that all definitions are actually referenced, and that theme.css vars appear
# in all 4 theme blocks.
set -e

# ── Allowlists ──────────────────────────────────────────────────────────────

# Variables set dynamically via JS or intentionally unreferenced
DEAD_VAR_ALLOWLIST="--font-sans --header-height"

# Variables that legitimately exist in only some theme blocks (e.g. hljs vars
# are scoped to their own selector blocks, not the 4 custom-property blocks)
BLOCK_ALLOWLIST=""

# ── Extract refs and defs ───────────────────────────────────────────────────

# All var(--xxx) references (POSIX ERE, works on macOS and Linux)
REFS=$(grep -oE 'var\(--[a-zA-Z0-9_-]+' frontend/style.css frontend/theme.css 2>/dev/null | sed 's/.*var(//' | sort -u)

# All --xxx: definitions (use perl for lookahead, portable)
DEFS=$(perl -nle 'print $1 if /^\s*(--[a-zA-Z0-9_-]+)\s*:/' frontend/theme.css frontend/style.css 2>/dev/null | sort -u)

# Add dynamic vars to definitions for the undefined-ref check
DEFS_PLUS_DYNAMIC="$DEFS"
for v in $DEAD_VAR_ALLOWLIST; do
    DEFS_PLUS_DYNAMIC=$(printf '%s\n%s' "$DEFS_PLUS_DYNAMIC" "$v")
done
DEFS_PLUS_DYNAMIC=$(echo "$DEFS_PLUS_DYNAMIC" | sort -u)

# ── Check A: Undefined references ──────────────────────────────────────────

MISSING=$(comm -23 <(echo "$REFS") <(echo "$DEFS_PLUS_DYNAMIC"))

if [ -n "$MISSING" ]; then
    echo "ERROR: Undefined CSS variables (referenced but never defined):"
    echo "$MISSING"
    exit 1
fi

echo "OK: All CSS variable references resolve to definitions."

# ── Check B: Dead definitions ───────────────────────────────────────────────

# Build allowlist lookup
DEAD_ALLOW=""
for v in $DEAD_VAR_ALLOWLIST; do
    DEAD_ALLOW=$(printf '%s\n%s' "$DEAD_ALLOW" "$v")
done
DEAD_ALLOW=$(echo "$DEAD_ALLOW" | sort -u)

DEAD=$(comm -23 <(echo "$DEFS") <(echo "$REFS"))
# Remove allowlisted vars
if [ -n "$DEAD_ALLOW" ]; then
    DEAD=$(comm -23 <(echo "$DEAD") <(echo "$DEAD_ALLOW"))
fi

if [ -n "$DEAD" ]; then
    echo "ERROR: Dead CSS variables (defined but never referenced via var()):"
    echo "$DEAD"
    exit 1
fi

echo "OK: No dead CSS variable definitions."

# ── Check C: 4-block completeness (theme.css only) ─────────────────────────

# Extract variables defined in theme.css (not style.css) and only from the
# 4 theme custom-property blocks, not from hljs selector blocks.
# Strategy: parse theme.css, track which block we're in, collect var names.

THEME_FILE="frontend/theme.css"

# We use perl to parse the file and emit "BLOCK_NAME\tVAR_NAME" pairs.
# The 4 blocks are identified by their opening patterns:
#   :root {                                -> root
#   prefers-color-scheme: light ... {      -> system-light (inside @media)
#   [data-theme="dark"] {                  -> dark
#   [data-theme="light"] {                 -> light
# We skip any block that contains .hljs (syntax highlighting blocks).

BLOCK_VARS=$(perl -e '
    use strict;
    use warnings;
    my $block = "";
    my $depth = 0;
    my $in_hljs = 0;

    while (<>) {
        # Detect block openings (only at depth 0 or 1 for the @media case)
        if (/^\s*:root\s*\{/ && $depth == 0) {
            $block = "root";
            $depth = 1;
            $in_hljs = 0;
            next;
        }
        if (/prefers-color-scheme:\s*light/ && $depth == 0) {
            # @media block — we will match the inner html:not block
            $depth = 1;
            $block = "";
            $in_hljs = 0;
            next;
        }
        if (/html:not\(\[data-theme\]\)\s*\{/ && $depth == 1 && $block eq "") {
            $block = "system-light";
            $depth = 2;
            $in_hljs = 0;
            next;
        }
        if (/\[data-theme="dark"\]\s*\{/ && $depth == 0 && !/\.hljs/) {
            $block = "dark";
            $depth = 1;
            $in_hljs = 0;
            next;
        }
        if (/\[data-theme="light"\]\s*\{/ && $depth == 0 && !/\.hljs/) {
            $block = "light";
            $depth = 1;
            $in_hljs = 0;
            next;
        }

        # Track braces for blocks we do not care about
        if ($block eq "" || $in_hljs) {
            my $opens = () = /\{/g;
            my $closes = () = /\}/g;
            $depth += $opens - $closes;
            $depth = 0 if $depth < 0;
            next;
        }

        # Inside a tracked block — detect hljs sub-blocks
        if (/\.hljs/) {
            $in_hljs = 1;
            next;
        }

        # Collect variable definitions
        if (/^\s*(--[a-zA-Z0-9_-]+)\s*:/) {
            print "$block\t$1\n";
        }

        # Track closing braces
        my $opens = () = /\{/g;
        my $closes = () = /\}/g;
        $depth += $opens - $closes;
        if ($depth <= 0) {
            $block = "";
            $depth = 0;
        }
    }
' "$THEME_FILE")

# Collect the union of all vars across the 4 blocks
ALL_THEME_VARS=$(echo "$BLOCK_VARS" | cut -f2 | sort -u)

# Build allowlist lookup for block check
BLOCK_ALLOW=""
for v in $BLOCK_ALLOWLIST; do
    BLOCK_ALLOW=$(printf '%s\n%s' "$BLOCK_ALLOW" "$v")
done
BLOCK_ALLOW=$(echo "$BLOCK_ALLOW" | sort -u)

ERRORS=""
for var in $ALL_THEME_VARS; do
    # Skip allowlisted vars
    if [ -n "$BLOCK_ALLOW" ] && echo "$BLOCK_ALLOW" | grep -qxF "$var"; then
        continue
    fi

    MISSING_BLOCKS=""
    for block in root system-light dark light; do
        if ! printf '%s\n' "$BLOCK_VARS" | grep -qxF "${block}	${var}"; then
            MISSING_BLOCKS="$MISSING_BLOCKS $block"
        fi
    done

    if [ -n "$MISSING_BLOCKS" ]; then
        ERRORS=$(printf '%s\n  %s missing from:%s' "$ERRORS" "$var" "$MISSING_BLOCKS")
    fi
done

if [ -n "$ERRORS" ]; then
    echo "ERROR: Theme variables not defined in all 4 blocks:$ERRORS"
    exit 1
fi

echo "OK: All theme variables defined in all 4 blocks."
