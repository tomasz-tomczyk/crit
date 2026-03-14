---
name: crit
description: Use when working with crit CLI commands, .crit.json files, addressing review comments, leaving inline code review comments, pushing reviews to GitHub PRs, or pulling PR comments locally. Covers crit comment, crit pull, crit push, .crit.json format, and resolution workflow.
---

# Crit CLI Reference

## .crit.json Format

After a crit review session, comments are in `.crit.json`. Comments are grouped per file with `start_line`/`end_line` referencing the source:

```json
{
  "files": {
    "path/to/file.md": {
      "comments": [
        {
          "id": "c1",
          "start_line": 5,
          "end_line": 10,
          "body": "Comment text",
          "author": "User Name",
          "resolved": false,
          "resolution_note": "Addressed by extracting to helper",
          "resolution_lines": "12-15"
        }
      ]
    }
  }
}
```

### Reading comments

- Comments are grouped per file with `start_line`/`end_line` referencing source lines in that file
- `resolved`: `false` or **missing** — both mean unresolved. Only `true` means resolved.
- Address each unresolved comment by editing the relevant file at the referenced location

### Resolving comments

After addressing a comment, update it in `.crit.json`:
- Set `"resolved": true`
- Optionally set `"resolution_note"` — brief description of what was done
- Optionally set `"resolution_lines"` — line range in the updated file where the change was made (e.g. `"12-15"`)

## Leaving Comments with crit comment CLI

Use `crit comment` to add inline review comments to `.crit.json` programmatically — no browser needed:

```bash
# Single line comment
crit comment [--author '<name>'] <path>:<line> '<body>'

# Multi-line comment (range)
crit comment [--author '<name>'] <path>:<start>-<end> '<body>'
```

Examples:

```bash
crit comment src/auth.go:42 'Missing null check on user.session — will panic if session expired'
crit comment src/handler.go:15-28 'This error is swallowed silently'
crit comment --author 'Claude' src/db.go:103 'Consider using a prepared statement here'
```

Rules:
- **Paths** are relative to the current working directory
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Body** is everything after the location argument — use single quotes to avoid shell interpretation
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates `.crit.json` automatically if it doesn't exist
- **Author** defaults to the `author` field in config (which falls back to `git config user.name`). Use `--author` to override

## GitHub PR Integration

```bash
crit pull [pr-number]              # Fetch PR review comments into .crit.json
crit push [--dry-run] [pr-number]  # Post .crit.json comments as a GitHub PR review
```

Requires `gh` CLI installed and authenticated. PR number is auto-detected from the current branch, or pass it explicitly.
