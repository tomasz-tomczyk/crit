---
description: Review code changes or a plan with crit inline comments
agent: build
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

If the `crit` skill is available, load it first.

## Step 1: Determine review mode

Pick whichever applies — don't ask for confirmation:

1. **User argument** — `$ARGUMENTS` provided (e.g., `/crit plan.md`) → review that file
2. **Recent plan** — no argument, but a plan was written earlier in this conversation → `crit <plan-file>`
3. **Branch review** — otherwise → bare `crit`. Auto-detects uncommitted changes or branch-vs-default-branch diff. Works on clean branches.
4. **PR / commit range** — user asked to review a specific GitHub PR or a commit range → `crit --pr <num|url>` or `crit --range <baseSHA>..<headSHA>` (boots crit in *range mode*, scoping the review to a fixed range of commits rather than the working tree).

## Step 2: Launch crit and block until review completes

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

Run `crit` in the foreground and block until it exits:

```bash
crit <plan-file>   # specific file
crit               # git mode
```

If a crit server is already running from earlier in this conversation, `crit` automatically connects to it. Starting from scratch, it spawns the daemon, opens the browser, and blocks until the user clicks "Finish Review".

`crit` prints the review URL on startup (e.g. `Started crit daemon at http://localhost:<port>`). Relay it verbatim:

> **"Crit is open at http://localhost:<port>. Leave inline comments, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read the review file early. Wait for the foreground command to finish — that is how you know the human is done reviewing.

## Step 3: Read the review output

When `crit` completes, its stdout includes the path to the review file (e.g. "Review comments are in /path/to/review.json"). Read it.

The file contains structured JSON. Three comment types:
- `review_comments` (top-level, `r_`-prefixed IDs) — general feedback
- File comments (per-file `comments` array, no `start_line`/`end_line`) — about the file as a whole
- Line comments (per-file `comments` array, with `start_line`/`end_line`) — about specific lines

Identify all comments where `resolved` is `false` or missing.

When a comment has a `quote`, `anchor`, or `drifted` field:
- `quote`: the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range
- `anchor`: use it to locate the current position of the content; line numbers may be stale after edits
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best

Before acting on a comment, check its `replies` array — if you have already replied, the reviewer may be following up conversationally rather than requesting a new code change.

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for
2. If it contains a suggestion block, apply that specific change
3. Revise the referenced file (plan or code file from the diff)
4. Reply with what you did: `crit comment --reply-to <id> --author 'OpenCode' '<what you did>'` (works for both file IDs `c_…` and review IDs `r_…`; reply bodies support markdown)
5. **Do not pass `--resolve`.** Resolving is the reviewer's call. Only add `--resolve` if the user explicitly asks.

Editing the plan file triggers Crit's live reload — the user sees changes in the browser immediately.

### Bulk replies

When replying to multiple comments at once, use `--json` for a single bulk call instead of one invocation per comment:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'OpenCode'
```

For multi-paragraph reply bodies, prefer `crit comment --json --file <path>` — a raw newline inside a JSON `"body"` string is invalid, and shell-quoted heredocs make that easy to slip in. Write the JSON to a temp file first, then point crit at it:

```bash
cat > /tmp/replies.json <<'EOF'
[
  {"reply_to": "c_a1b2c3", "body": "Fixed.\n\nDetails: split helper, added null guard."}
]
EOF
crit comment --json --file /tmp/replies.json --author 'OpenCode'
```

`--file -` reads stdin (same as the default).

**If there are zero review comments**: inform the user no changes were requested and stop.

## Step 5: Signal completion and start next round

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

When `crit` exited in Step 2, it printed `Next round: crit <args>` to stdout if more rounds are expected. Run the command crit printed after `Next round:` — verbatim — in the foreground and block until it exits.

On subsequent calls, `crit` automatically signals round-complete first, then blocks until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, return to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## Sharing

If the user asks for a URL, a shareable link, or a QR code for the review:

```bash
crit share <file>
```

**Always relay the full output to the user** — copy the URL (and QR code if `--qr` was used) directly into your response. Don't make them dig through tool output.

To remove a shared review:

```bash
crit unpublish
```

### QR caveat

Only use `--qr` in real terminal environments with monospace rendering. Skip it in mobile apps or web chat UIs — Unicode block characters won't render.

```bash
crit share --qr <file>
```
