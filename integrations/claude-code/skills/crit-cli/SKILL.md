---
name: crit-cli
description: Use when working with crit CLI commands, review files, addressing review comments, leaving inline code review comments, sharing reviews via crit share/unpublish, pushing reviews to GitHub PRs, or pulling PR comments locally. Covers crit comment, crit share, crit unpublish, crit pull, crit push, review file format, and resolution workflow.
user-invocable: false
---

# Crit CLI Reference

> If a plan was just written and the user said `/crit` or `crit`, invoke the `/crit` command — do not use this reference skill. This skill covers CLI operations like `crit comment`, `crit pull/push`, and `crit share`.

## Review File Format

After a crit review session, comments are in the review file (see `crit status` for the path). Comments have three scopes:

- **Line comments** (`scope: "line"`) — tied to specific lines in a file, stored in `files.<path>.comments`
- **File comments** (`scope: "file"`) — about a file overall, stored in `files.<path>.comments` with `start_line: 0`
- **Review comments** (`scope: "review"`) — general feedback not tied to any file, stored in `review_comments`

```json
{
  "review_comments": [
    {
      "id": "r_f1e2d3",
      "body": "Overall the architecture looks good",
      "scope": "review",
      "author": "User Name",
      "resolved": false,
      "replies": [
        { "id": "rp_b4a5c6", "body": "Thanks, addressed the minor issues", "author": "Claude" }
      ]
    }
  ],
  "files": {
    "path/to/file.go": {
      "comments": [
        {
          "id": "c_a1b2c3",
          "start_line": 5,
          "end_line": 10,
          "body": "Comment text",
          "quote": "the specific words selected",
          "anchor": "The sessions table needs a complete rewrite...",
          "author": "User Name",
          "resolved": false,
          "replies": [
            { "id": "rp_c7d8e9", "body": "Fixed by extracting to helper", "author": "Claude" }
          ]
        }
      ]
    }
  }
}
```

### Reading comments

- **Line comments** are grouped per file with `start_line`/`end_line` referencing source lines in that file
- **File comments** are in the same per-file array but have `start_line: 0, end_line: 0, scope: "file"`
- **Review comments** are in the top-level `review_comments` array (not tied to any file)
- `quote` (optional): the specific text the reviewer selected — narrows the comment's scope within the line range. When present, focus your changes on the quoted text rather than the entire line range
- `anchor` (present on line comments): the full text of the commented lines when the comment was placed. When your edits shift line numbers, use the anchor text to locate the current position of the content rather than trusting `start_line`/`end_line` which may be stale after edits
- `drifted`: if `true`, the original content was removed or heavily rewritten — the line numbers are approximate at best
- `resolved`: `false` or **missing** — both mean unresolved. Only `true` means resolved.
- Address each unresolved comment by editing the relevant file at the referenced location
- Before acting on a comment, check its `replies` array — if you have already replied, the reviewer may be following up conversationally rather than requesting a new code change

### Replying to comments

After addressing a comment, reply to it using the CLI:

```bash
crit comment --reply-to c_a1b2c3 --author 'Claude Code' 'Fixed by extracting to helper'
crit comment --reply-to r_f1e2d3 --author 'Claude Code' 'All issues addressed'
```

This adds a reply to the comment thread. Works for both file comment IDs (e.g. `c_a1b2c3`) and review comment IDs (e.g. `r_f1e2d3`). Only use `--resolve` when the user explicitly asks you to resolve a comment — never resolve proactively.

**Multi-file disambiguation**: Comment IDs are unique per session, but if you encounter an error like "comment found in multiple files", use `--path` to specify which file:

```bash
crit comment --reply-to c_a1b2c3 --path src/auth.go --author 'Claude Code' 'Fixed the null check'
```

In `--json` bulk mode, use the `file` field on the reply entry:

```bash
echo '[{"reply_to": "c_a1b2c3", "file": "src/auth.go", "body": "Fixed"}]' | crit comment --json --author 'Claude Code'
```

Review-level comment IDs (`r_XXXXXX`) are globally unique and never need disambiguation.

### Plan mode comments

When reviewing plans (via `crit plan` or the ExitPlanMode hook), the review file is stored in `~/.crit/plans/<slug>/`. Use `--plan <slug>` so `crit comment` finds the right file:

```bash
crit comment --plan my-plan-2026-03-23 --reply-to c_a1b2c3 --author 'Claude Code' 'Updated the plan'
```

The `--plan` flag resolves to the plan storage directory automatically. The slug is shown in the review feedback prompt. **Always use `--plan` when responding to plan review comments** — without it, `crit comment` looks in the project root and won't find the comments.

## Leaving Comments with crit comment CLI

Use `crit comment` to add review comments to the review file programmatically — no browser needed:

```bash
# Review-level comment (general feedback, not tied to any file)
crit comment --author 'Claude Code' '<body>'

# File-level comment (about a file overall, no line numbers)
crit comment --author 'Claude Code' <path> '<body>'

# Line comment (single line)
crit comment --author 'Claude Code' <path>:<line> '<body>'

# Line comment (range)
crit comment --author 'Claude Code' <path>:<start>-<end> '<body>'

# Reply to an existing comment
crit comment --reply-to <id> --author 'Claude Code' '<body>'
```

Examples:

```bash
crit comment --author 'Claude Code' 'Overall architecture looks solid'
crit comment --author 'Claude Code' src/auth.go 'This file needs restructuring'
crit comment --author 'Claude Code' src/auth.go:42 'Missing null check on user.session — will panic if session expired'
crit comment --author 'Claude Code' src/handler.go:15-28 'This error is swallowed silently'
crit comment --reply-to c_a1b2c3 --author 'Claude Code' 'Added null check on line 42'
crit comment --reply-to r_f1e2d3 --author 'Claude Code' 'All issues addressed'
```

Rules:
- **Always use `--author 'Claude'`** (or your agent name) so comments are attributed correctly
- **Always use single quotes** for the body — double quotes will break on backticks and special characters
- **Paths** are relative to the current working directory
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates the review file automatically if it doesn't exist
- **Do NOT run `crit` after leaving comments** — that triggers a new review round

### Bulk commenting (recommended for multiple comments)

When leaving 3+ comments, use `--json` to add them all in one atomic operation:

```bash
echo '[
  {"body": "overall feedback", "scope": "review"},
  {"path": "session.go", "body": "restructure", "scope": "file"},
  {"file": "src/auth.go", "line": 42, "body": "Missing null check"},
  {"file": "src/auth.go", "line": "50-55", "body": "Extract to helper"},
  {"reply_to": "c_a1b2c3", "body": "Fixed — added null check"},
  {"reply_to": "r_f1e2d3", "body": "Done"}
]' | crit comment --json --author 'Claude Code'
```

JSON schema per entry:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | string | yes (line comment) / no (reply) | Relative file path. For replies, disambiguates when the same ID exists in multiple files |
| `path` | string | alt for `file` | Alias for `file`; when used with no `line`, infers file-level |
| `line` | int/string | yes (line comment) | Start line (`42`) or range (`"45-47"`) |
| `end_line` | int | no | End line (defaults to `line`) |
| `body` | string | yes | Comment text |
| `author` | string | no | Per-entry override (falls back to `--author`) |
| `scope` | string | no | `"review"`, `"file"`, or omit to infer from context |
| `reply_to` | string | yes (reply) | Comment ID (e.g. `"c_a1b2c3"` or `"r_f1e2d3"`) |
| `resolve` | bool | no | Only set when user explicitly asks to resolve — never resolve proactively |

Scope inference when `scope` is omitted:
- Has `reply_to` → reply
- No `file`/`path` and no `line` → review-level
- Has `path` but no `line` → file-level
- Has `file`/`path` and `line` → line-level

Benefits over individual `crit comment` calls:
- **Atomic** — one write to the review file, no partial state
- **Faster** — single process invocation instead of N
- **Safer** — no race conditions with concurrent crit processes

## GitHub PR Integration

```bash
crit pull [pr-number]                                    # Fetch PR review comments into the review file
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]  # Post review comments as a GitHub PR review
```

Requires `gh` CLI installed and authenticated. PR number is auto-detected from the current branch, or pass it explicitly.

Event types for `--event`: `comment` (default), `approve`, `request-changes`. Use `-m` to add a review-level body message.

## Sharing Reviews

If the user asks for a URL, a link, to share their review, or to show a QR code, use `crit share`:

```bash
crit share <file> [file...]   # Upload and print URL
crit share --qr <file>        # Also print QR code (terminal only)
crit unpublish                # Remove shared review
```

Examples:

```bash
crit share <file>                                # Share a single file
crit share <file1> <file2>                       # Share multiple files
crit share --share-url https://crit.md <file>  # Explicit share URL
```

Rules:
- **No server needed** — `crit share` reads files directly from disk
- **`--qr` is terminal-only** — only use when the user has a real terminal with monospace font rendering. Do not use in mobile apps (e.g. Claude Code mobile), web chat UIs, or any environment where Unicode block characters won't render correctly
- **Comments included** — if the review file exists, comments for the shared files are included automatically
- **Relay the output** — always copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response to the user. Do not make them dig through tool output
- **State persisted** — share URL and delete token are saved to the review file
- **Unpublish reads the review file** — uses the stored delete token to remove the review
