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
- **Relay the output** — always copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response to the user. Do not make them dig through tool output
- **State persisted** — share URL and delete token are saved to `.crit.json`
- **Unpublish reads `.crit.json`** — uses the stored delete token to remove the review

## Guardrails

- Do not continue past the review step until the user confirms they are done.
- Treat `.crit.json` as the source of truth for line references and comment status.
- If there are no unresolved comments, tell the user no changes were requested and stop.
