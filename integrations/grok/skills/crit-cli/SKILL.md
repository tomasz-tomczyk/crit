---
name: crit-cli
description: Use when an agent needs to author or reply to crit inline comments programmatically (including multi-agent workflows commenting on shared code/plans/docs/proposals), publish or unpublish a crit review with crit share, sync a crit review to or from a GitHub PR, or read/interpret a crit review JSON file. Covers crit comment, crit share, crit unpublish, crit pull, crit push, review file format, and resolution workflow. Not for invoking an interactive review loop — that's the `crit` skill.
user-invocable: false
---

# Crit CLI Reference

> If a plan was just written and the user said "crit" or "review", use the `$crit` skill instead — it covers the full review loop. This skill covers CLI operations like `crit comment`, `crit pull`/`push`, and `crit share`.

Comments have three scopes:

- **Line comments** (`scope: "line"`) — tied to specific lines, stored in `files.<path>.comments`
- **File comments** (`scope: "file"`) — about a file overall, stored in `files.<path>.comments` with `start_line: 0`
- **Review comments** (`scope: "review"`) — general feedback, stored in the top-level `review_comments` array

The review file path is shown by `crit status`.

Use `read_file` on the path printed by `crit`. Example structure:

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
        { "id": "rp_b4a5c6", "body": "Thanks, addressed the minor issues", "author": "Grok" }
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
          "replies": [ ... ]
        }
      ]
    }
  }
}
```

Field rules:
- `resolved`: `false` or **missing** both mean unresolved. Only `true` means resolved.
- `quote` (optional): the exact text the reviewer highlighted.
- `anchor` (line comments): the full text of the commented lines at the time the comment was placed. Use the anchor to locate content after edits.
- `drifted: true`: content was removed or heavily rewritten — treat line numbers as approximate.
- Before acting on a comment, always check its `replies` array.

<important if="you are authoring or replying to comments via crit comment">

Use `run_terminal_cmd` with the following patterns. Always pass `--author 'Grok'`.

```bash
# Review-level (general feedback)
crit comment --author 'Grok' 'Overall feedback here'

# File-level (whole file, no line numbers)
crit comment --author 'Grok' path/to/file.md 'The whole file needs X'

# Line (single line or range)
crit comment --author 'Grok' path/to/file.go:42 'Missing null check'
crit comment --author 'Grok' path/to/file.go:50-55 'Extract to helper'

# Reply to an existing comment
crit comment --reply-to <id> --author 'Grok' 'Fixed — added the helper and tests'
```

Hard rules:
- **Always pass `--author 'Grok'`**.
- **Always single-quote the body** in the shell command (double quotes break on backticks, `$`, etc.).
- Line numbers are 1-indexed file lines on disk (not diff lines).
- Reply bodies support full markdown.
- Only pass `--resolve` when the user explicitly asks you to.
</important>

<important if="you are leaving 3+ comments in one operation">

Use `--json` for atomicity and speed. Two ways to feed JSON:

```bash
# Short bodies — pipe via stdin
echo '[
  {"body": "overall feedback", "scope": "review"},
  {"path": "session.go", "body": "restructure the round logic", "scope": "file"},
  {"file": "src/auth.go", "line": 42, "body": "Missing null check"},
  {"file": "src/auth.go", "line": "50-55", "body": "Extract to helper"},
  {"reply_to": "c_a1b2c3", "body": "Fixed — added null check"}
]' | crit comment --json --author 'Grok'
```

**Prefer `--file <path>` for any multi-paragraph body** (shell quoting of newlines in JSON is fragile). Write the JSON with `write`, then point crit at it:

```bash
crit comment --json --file /tmp/replies.json --author 'Grok'
```

`--file -` reads stdin (same as omitting the flag).

Per-entry schema:

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `file` / `path` | string | line/file comments | Relative path. `path` alone → file-level. |
| `line` | int/string | line comments | `42` or `"45-47"` |
| `end_line` | int | optional | Defaults to `line` |
| `body` | string | always | |
| `author` | string | optional | Per-entry override |
| `scope` | string | optional | `"review"` / `"file"` — usually inferred |
| `reply_to` | string | replies | Comment ID (`c_…` or `r_…`) |
| `resolve` | bool | optional | Only when user explicitly asks |

Scope inference (when `scope` omitted): has `reply_to` → reply; no `file`/`path` and no `line` → review-level; `path` but no `line` → file-level; `file`/`path` + `line` → line.
</important>

<important if="crit comment errored with 'comment found in multiple files'">

Comment IDs are unique per session, but the same ID can appear in multiple files. Disambiguate with `--path`:

```bash
crit comment --reply-to c_a1b2c3 --path src/auth.go --author 'Grok' 'Fixed the null check'
```

In `--json` mode, set the `file` field on the entry. Review-level IDs (`r_…`) are globally unique.
</important>

<important if="you are responding to plan-mode comments (review file under ~/.crit/plans/)">

Plan reviews (via `crit plan` or the `exit_plan_mode` hook) store the review file in `~/.crit/plans/<slug>/`. **Always pass `--plan <slug>`** — without it `crit comment` looks in the project root and will not find the comments. The slug is shown in the review feedback prompt and in the output of `crit plan-hook`.

```bash
crit comment --plan my-plan-2026-05-14 --reply-to c_a1b2c3 --author 'Grok' 'Updated the plan'
```

When you are in a Grok plan-mode session, the plan file itself lives at `~/.grok/sessions/<cwd>/<session-id>/plan.md`. The `--plan <slug>` flag tells `crit comment` which Crit-managed review file to write to.
</important>

<important if="you are syncing with a GitHub PR (pull or push)">

```bash
crit pull [pr-number]                                    # Fetch PR review comments into the review file
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]   # Post review comments as a GitHub PR review
```

Requires the `gh` CLI installed and authenticated. PR number is auto-detected from the current branch (or you can pass it explicitly).

`--event` values: `comment` (default), `approve`, `request-changes`. `-m` adds a review-level body message.
</important>

<important if="the user asked to share, get a URL, get a QR code, or unpublish a review">

```bash
crit share <file> [file...]                          # Upload and print URL
crit share --qr <file>                               # Also print QR code (terminal only)
crit share --org <slug> <file>                       # Share under an organization
crit share --org <slug> --visibility unlisted <file> # Org share with explicit visibility
crit unpublish                                       # Remove shared review
```

- **No server needed** — reads files directly from disk. If a review file exists, comments for the shared files are included automatically.
- **Always relay the output** — copy the URL (and QR if used) into your response.
- **`--qr` is terminal-only** — skip in web/chat UIs where block characters won't render.
- **`--org <slug>`** shares under an organization. Visibility defaults to `organization` (members only). Override with `--visibility` (`organization`, `unlisted`, `public`).
- **Unpublish** uses the persisted delete token in the review file — no extra args needed.
</important>

## Review file location quick reference

- Normal git/files mode: `~/.crit/reviews/<key>.json`
- Plan mode (via `crit plan` or hook): `~/.crit/plans/<slug>/review.json` (the `current.md` symlink points at the latest plan version)
- The exact path is always printed by the `crit` command and by `crit status --json`.

Use `read_file` on the printed path, then act on the `review_comments` and per-file `comments` arrays as described above.

This reference skill is automatically available whenever the agent needs to manipulate Crit comments or reviews programmatically. Pair it with the main `crit` skill when the user wants the interactive browser review experience.
