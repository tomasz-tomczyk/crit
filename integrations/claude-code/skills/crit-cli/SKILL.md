---
name: crit-cli
description: Use when an agent needs to author or reply to crit inline comments programmatically (including multi-agent workflows commenting on shared code/plans/docs/proposals), publish or unpublish a crit review with crit share, sync a crit review to or from a GitHub PR, or read/interpret a crit review JSON file. Covers crit comment, crit share, crit unpublish, crit pull, crit push, review file format, and resolution workflow. Not for invoking an interactive review loop — that's the `/crit` command.
user-invocable: false
---

# Crit CLI Reference

> If a plan was just written and the user said `/crit` or `crit`, invoke the `/crit` command — do not use this reference skill. This skill covers CLI operations like `crit comment`, `crit pull/push`, and `crit share`.

Comments have three scopes:

- **Line comments** (`scope: "line"`) — tied to specific lines, stored in `files.<path>.comments`
- **File comments** (`scope: "file"`) — about a file overall, stored in `files.<path>.comments` with `start_line: 0`
- **Review comments** (`scope: "review"`) — general feedback, stored in the top-level `review_comments` array

The review file path is shown by `crit status`.

<important if="you are reading or parsing the review file">

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

Field rules:
- `resolved`: `false` or **missing** — both mean unresolved. Only `true` means resolved.
- `quote` (optional): the specific text the reviewer selected — narrows scope within the line range. Focus changes on the quoted text rather than the entire range.
- `anchor` (line comments): full text of the commented lines when placed. When edits shift line numbers, locate content by anchor rather than trusting `start_line`/`end_line`.
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best.
- Unresolved comments may have `replies` — read them before acting.
</important>

<important if="you are authoring or replying to comments via crit comment">

```bash
# Review-level (general feedback)
crit comment --author 'Claude Code' '<body>'

# File-level (whole file, no line numbers)
crit comment --author 'Claude Code' <path> '<body>'

# Line (single line or range)
crit comment --author 'Claude Code' <path>:<line> '<body>'
crit comment --author 'Claude Code' <path>:<start>-<end> '<body>'

# Reply to an existing comment
crit comment --reply-to <id> --author 'Claude Code' '<body>'
```

Hard rules:
- **Always pass `--author 'Claude Code'`** (or your agent name) so comments are attributed correctly.
- **Always single-quote the body** — double quotes break on backticks and shell metachars.
- **Line numbers reference the file on disk** (1-indexed), not diff line numbers.
- **Reply bodies support markdown** — use code fences and inline code where helpful.
- **Only pass `--resolve` when the user explicitly asks.** Never resolve proactively.
</important>

<important if="you are leaving 3+ comments in one operation">

Use `--json` for atomicity (single write, no partial state) and speed (one process). Two ways to feed the JSON:

```bash
# Short, single-line bodies — pipe via stdin:
echo '[
  {"body": "overall feedback", "scope": "review"},
  {"path": "session.go", "body": "restructure", "scope": "file"},
  {"file": "src/auth.go", "line": 42, "body": "Missing null check"},
  {"file": "src/auth.go", "line": "50-55", "body": "Extract to helper"},
  {"reply_to": "c_a1b2c3", "body": "Fixed — added null check"},
  {"reply_to": "r_f1e2d3", "body": "Done"}
]' | crit comment --json --author 'Claude Code'
```

**Prefer `--file <path>` for any multi-paragraph body.** Shell-quoted JSON breaks the moment a `"body"` string contains a raw newline — JSON forbids them, and the shell happily passes them through. Use the Write tool to author the JSON to a temp file, then point crit at it:

```bash
# After Write-ing /tmp/replies.json:
crit comment --json --file /tmp/replies.json --author 'Claude Code'
```

`--file -` reads stdin (same as omitting the flag).

Per-entry schema:

| Field | Type | Required | Notes |
|---|---|---|---|
| `file` / `path` | string | line/file comments | Relative path. `path` alone (no `line`) → file-level. |
| `line` | int/string | line comments | `42` or `"45-47"` |
| `end_line` | int | optional | Defaults to `line` |
| `body` | string | always | |
| `author` | string | optional | Per-entry override; falls back to `--author` |
| `scope` | string | optional | `"review"` / `"file"` — usually inferred |
| `reply_to` | string | replies | Comment ID (`c_…` or `r_…`) |
| `resolve` | bool | optional | Only when user explicitly asks |

Scope inference (when `scope` omitted): has `reply_to` → reply; no `file`/`path` and no `line` → review-level; `path` but no `line` → file-level; `file`/`path` + `line` → line.
</important>

<important if="crit comment errored with 'comment found in multiple files'">
Comment IDs are unique per session, but the same ID can collide across files. Disambiguate with `--path`:

```bash
crit comment --reply-to c_a1b2c3 --path src/auth.go --author 'Claude Code' 'Fixed the null check'
```

In `--json` mode, set the `file` field on the entry. Review-level IDs (`r_…`) are globally unique and never need this.
</important>

<important if="you are responding to plan-mode comments (review file under ~/.crit/plans/)">
Plan reviews (via `crit plan` or the ExitPlanMode hook) store the review file in `~/.crit/plans/<slug>/`. **Always pass `--plan <slug>`** — without it, `crit comment` looks in the project root and won't find the comments. The slug is shown in the review feedback prompt.

```bash
crit comment --plan my-plan-2026-03-23 --reply-to c_a1b2c3 --author 'Claude Code' 'Updated the plan'
```
</important>

<important if="you are syncing with a GitHub PR (pull or push)">

```bash
crit pull [pr-number]                                    # Fetch PR review comments into the review file
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]   # Post review comments as a GitHub PR review
```

Requires `gh` CLI installed and authenticated. PR number is auto-detected from the current branch.

`--event` values: `comment` (default), `approve`, `request-changes`. `-m` adds a review-level body message.
</important>

<important if="the user asked to share, get a URL, get a QR code, or unpublish a review">

```bash
crit share <file> [file...]                          # Upload and print URL
crit share --qr <file>                               # Also print QR code (terminal only)
crit share --org <slug> <file>                       # Share under an organization
crit share --org <slug> --visibility unlisted <file> # Org share with explicit visibility
crit unpublish [file...]                              # Remove shared review
```

- **No server needed** — reads files directly from disk. If a review file exists, comments for the shared files are included automatically.
- **Always relay the output** — copy the URL (and QR if used) into your response. Don't make the user dig through tool output.
- **`--qr` is terminal-only** — skip in mobile apps, web chat UIs, or anywhere Unicode block characters won't render correctly.
- **`--org <slug>`** shares under an organization. Visibility defaults to `organization` (members only). Override with `--visibility` (`organization`, `unlisted`, `public`).
- **Unpublish uses the persisted delete token** in the review file — no extra args needed.
</important>
