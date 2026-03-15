---
name: crit
description: Use when working with crit CLI commands, .crit.json files, addressing review comments, leaving inline code review comments, sharing reviews via crit share/unpublish, pushing reviews to GitHub PRs, or pulling PR comments locally. Covers crit comment, crit share, crit unpublish, crit pull, crit push, .crit.json format, and resolution workflow.
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
crit comment --author 'Claude' <path>:<line> '<body>'

# Multi-line comment (range)
crit comment --author 'Claude' <path>:<start>-<end> '<body>'
```

Examples:

```bash
crit comment --author 'Claude' src/auth.go:42 'Missing null check on user.session — will panic if session expired'
crit comment --author 'Claude' src/handler.go:15-28 'This error is swallowed silently'
```

Rules:
- **Always use `--author 'Claude'`** (or your agent name) so comments are attributed correctly
- **Always use single quotes** for the body — double quotes will break on backticks and special characters
- **Paths** are relative to the current working directory
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates `.crit.json` automatically if it doesn't exist
- **Do NOT run `crit go` after leaving comments** — that triggers a new review round. Only the `/crit` command flow uses `crit go`, and only after addressing (not leaving) comments

## GitHub PR Integration

```bash
crit pull [pr-number]              # Fetch PR review comments into .crit.json
crit push [--dry-run] [pr-number]  # Post .crit.json comments as a GitHub PR review
```

Requires `gh` CLI installed and authenticated. PR number is auto-detected from the current branch, or pass it explicitly.

## Sharing Reviews

If the user asks to share a review, get a link, get a URL, or show a QR code, use `crit share`:

```bash
crit share <file> [file...]   # Upload and print URL
crit share --qr plan.md       # Also print QR code (terminal only)
crit unpublish                # Remove shared review
```

Examples:

```bash
crit share plan.md                           # Share a single file
crit share plan.md src/main.go               # Share multiple files
crit share --share-url https://crit.live plan.md  # Explicit share URL
```

Rules:
- **No server needed** — `crit share` reads files directly from disk
- **`--qr` is terminal-only** — only use in environments with a real terminal (not mobile apps, not web UIs)
- **Comments included** — if `.crit.json` exists, comments for the shared files are included automatically
- **URL printed to stdout** — the share URL is the only output on stdout (safe for piping)
- **State persisted** — share URL and delete token are saved to `.crit.json`
- **Unpublish reads `.crit.json`** — uses the stored delete token to remove the review
