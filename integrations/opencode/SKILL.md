---
name: crit
description: Use when working with crit CLI commands, .crit.json files, addressing review comments, leaving inline code review comments, sharing reviews via crit share/unpublish, pushing reviews to GitHub PRs, or pulling PR comments locally. Covers crit comment, crit share, crit unpublish, crit pull, crit push, .crit.json format, and resolution workflow.
compatibility: opencode
---

## What I do

- Launch Crit for a plan file or the current git diff.
- Wait for the user to review changes in the browser.
- Read `.crit.json` and address unresolved inline comments.
- Signal the next review round with `crit go <port>` when edits are done.
- Leave inline review comments programmatically with `crit comment`.
- Sync reviews with GitHub PRs via `crit pull` and `crit push`.

## When to use me

Use this when the user asks to review a plan, spec, or code changes in Crit, when project instructions require a Crit pass before accepting non-trivial changes, when leaving inline comments on code, or when syncing reviews with GitHub PRs.

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
          "quote": "the specific words selected",
          "author": "User Name",
          "resolved": false,
          "replies": [
            { "id": "c1-r1", "body": "Fixed by extracting to helper", "author": "Claude" }
          ]
        }
      ]
    }
  }
}
```

### Reading comments

- Comments are grouped per file with `start_line`/`end_line` referencing source lines in that file
- `quote` (optional): the specific text the reviewer selected — narrows the comment's scope within the line range. When present, focus your changes on the quoted text rather than the entire line range
- `resolved`: `false` or **missing** — both mean unresolved. Only `true` means resolved.
- Address each unresolved comment by editing the relevant file at the referenced location

### Resolving comments

After addressing a comment, reply to it using the CLI:

```bash
crit comment --reply-to c1 --resolve 'Fixed by extracting to helper'
```

This adds a reply to the comment thread and marks it resolved. You can also reply without resolving (omit `--resolve`) if discussion is ongoing.

## Leaving Comments with crit comment CLI

Use `crit comment` to add inline review comments to `.crit.json` programmatically — no browser needed:

```bash
# Single line comment
crit comment --author 'Claude' <path>:<line> '<body>'

# Multi-line comment (range)
crit comment --author 'Claude' <path>:<start>-<end> '<body>'

# Reply to an existing comment (with optional --resolve)
crit comment --reply-to <id> --author 'Claude' '<body>'
crit comment --reply-to <id> --resolve --author 'Claude' '<body>'
```

Rules:
- **Always use `--author 'Claude'`** (or your agent name) so comments are attributed correctly
- **Always use single quotes** for the body — double quotes will break on backticks and special characters
- **Paths** are relative to the current working directory
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates `.crit.json` automatically if it doesn't exist
- **Do NOT run `crit go` after leaving comments** — that triggers a new review round

## GitHub PR Integration

```bash
crit pull [pr-number]              # Fetch PR review comments into .crit.json
crit push [--dry-run] [pr-number]  # Post .crit.json comments as a GitHub PR review
```

Requires `gh` CLI installed and authenticated. PR number is auto-detected from the current branch, or pass it explicitly.

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
crit share --share-url https://crit.live <file>  # Explicit share URL
```

Rules:
- **No server needed** — `crit share` reads files directly from disk
- **`--qr` is terminal-only** — only use when the user has a real terminal with monospace font rendering. Do not use in mobile apps (e.g. Claude Code mobile), web chat UIs, or any environment where Unicode block characters won't render correctly
- **Comments included** — if `.crit.json` exists, comments for the shared files are included automatically
- **Relay the output** — always copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response to the user. Do not make them dig through tool output
- **State persisted** — share URL and delete token are saved to `.crit.json`
- **Unpublish reads `.crit.json`** — uses the stored delete token to remove the review

## Guardrails

- Do not continue past the review step until the user confirms they are done.
- Treat `.crit.json` as the source of truth for line references and comment status.
- If there are no unresolved comments, tell the user no changes were requested and stop.
