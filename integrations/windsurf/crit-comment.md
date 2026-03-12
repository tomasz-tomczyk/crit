# Leaving Comments with Crit

Use `crit comment` to add inline review comments to `.crit.json`. Comments are displayed in crit's browser UI for interactive review.

## Syntax

```bash
# Single line comment
crit comment <path>:<line> '<body>'

# Multi-line comment (range)
crit comment <path>:<start>-<end> '<body>'
```

## Examples

```bash
crit comment src/auth.go:42 'Missing null check on user.session — will panic if session expired'
crit comment src/handler.go:15-28 'This error is swallowed silently. The catch block returns ok but the caller expects an error on failure.'
crit comment src/db.go:103 'Consider using a prepared statement here to avoid SQL injection'
```

## Rules

- **Paths** are relative to your current working directory
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Body** is everything after the location argument — use single quotes to avoid shell interpretation
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates `.crit.json` automatically if it doesn't exist
