---
name: crit
description: "Review code changes, a plan, a live page (running dev server), or a local HTML file with crit inline comments. Use when asked to review code, a plan, a diff, a running web app, or when you want structured human feedback on your work."
---

# Review with Crit

Review and revise code changes, plans, live pages (running dev servers, staging URLs), or local HTML files using `crit` for inline comment review.

## Step 1: Pass arguments to `crit`

The CLI auto-detects the review mode from its arguments. **Do not ask the user which mode to use.** Pass arguments through:

```
crit <arguments>               # file, dir, URL, .html — CLI auto-detects mode
crit --pr <num|url>            # GitHub PR (range mode)
crit --range <base>..<head>    # commit range (range mode)
crit                           # no args → branch diff
```
If no arguments, check conversation context:

1. A plan file was written earlier in this conversation → `crit <plan-file>`
2. Otherwise → bare `crit` (branch diff)

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

When a comment has these fields:
- `quote`: the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range
- `anchor`: use it to locate the current position of the content; line numbers may be stale after edits
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for
2. If it contains a suggestion block, apply that specific change
3. Revise the referenced file (plan or code file from the diff)
4. Reply with what you did: `crit comment --reply-to <id> --author 'Hermes' '<what you did>'` (reply bodies support markdown)
5. **Do not pass `--resolve`.** Resolving is the reviewer's call. Only add `--resolve` if the user explicitly asks.

Editing the plan file triggers Crit's live reload — the user sees changes in the browser immediately.

### When replying to multiple comments

Use `--json` for a single bulk call instead of one invocation per comment:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Hermes'
```

**If there are zero review comments**: inform the user no changes were requested and stop.

## Step 5: Signal completion and start next round

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

When Step 2's `crit` command exits with feedback, it prints `Next round: crit <args>` to stdout. Run that command verbatim — the daemon is keyed by args, so mismatched args spawn a new daemon instead of reconnecting.

On subsequent calls, `crit` automatically signals round-complete first, then blocks until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, return to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## Sharing

If the user asks for a URL, a shareable link, or to share the review:

```bash
crit share <file>
```

**Always relay the full output to the user** — copy the URL directly into your response. Don't make them dig through tool output.

To remove a shared review:

```bash
crit unpublish
```

### QR codes

Only use `--qr` in real terminal environments with monospace rendering. Skip it in mobile apps or web chat UIs — Unicode block characters won't render.

```bash
crit share --qr <file>
```
