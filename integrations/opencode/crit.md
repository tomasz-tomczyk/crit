---
description: Review code changes or a plan with crit inline comments
agent: build
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Determine review mode

Pick whichever applies — don't ask for confirmation:

1. **User argument** — `$ARGUMENTS` provided (e.g., `/crit plan.md`) → review that file
2. **Recent plan** — no argument, but a plan was written earlier in this conversation → `crit <plan-file>`
3. **Branch review** — otherwise → bare `crit`. Auto-detects uncommitted changes or branch-vs-default-branch diff. Works on clean branches.
4. **PR / commit range** — user asked to review a specific GitHub PR, GitLab MR, or a commit range → `crit --pr <num|url>`, `crit --mr <iid|url>`, or `crit --range <baseSHA>..<headSHA>` (boots crit in *range mode*, scoping the review to a fixed range of commits rather than the working tree).

## Step 2: Launch crit and wait for review completion

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

Inspect the available Bash/shell tool schema. Use the same strategy for the initial review and every subsequent round.

### Background-capable shell

If the Bash/shell tool exposes a `background` boolean, launch `crit` with `background: true` and no `timeout` argument. Background commands default to no timeout. Do not append `&`, use `nohup`, or wrap the command in another backgrounding mechanism.

```text
Shell tool call:
- command: crit <plan-file>   # specific file
- background: true

Shell tool call:
- command: crit               # git mode
- background: true
```

The tool returns immediately and notifies you automatically when `crit` finishes. Do not poll, sleep, read the review file early, or launch a duplicate command while it is running.

Tell the user:

> **"Crit is running in the background and will open in your browser. Leave inline comments, then click Finish Review."**

End the current response after launching it. When the completion notification arrives, continue at Step 3.

### Foreground-only shell

If the Bash/shell tool does not expose `background`, run `crit` in the foreground with an explicit 24-hour timeout. The timeout is a tool argument in milliseconds, not part of the shell command:

```text
Bash tool call:
- command: crit <plan-file>   # specific file
- timeout: 86400000

Bash tool call:
- command: crit               # git mode
- timeout: 86400000
```

Always pass `timeout: 86400000` to every blocking `crit` invocation. Do not rely on the shell tool's default timeout. Wait for the command to exit before continuing.

If a crit server is already running from earlier in this conversation, `crit` automatically connects to it. Starting from scratch, it spawns the daemon, opens the browser, and waits until the user clicks "Finish Review".

In foreground mode, `crit` prints the review URL on startup (e.g. `Started crit daemon at http://localhost:<port>`). Relay it verbatim:

> **"Crit is open at http://localhost:<port>. Leave inline comments, then click Finish Review."**

In background mode, intermediate output may not be available before completion; rely on Crit opening the browser automatically rather than polling for the URL.

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Command completion is how you know the human is done reviewing.

## Step 3: Read the review output

When `crit` completes, read **stdout** and follow its instructions. Check **stderr** for `approved: true` or `approved: false`.

When a comment has a `quote`, `anchor`, or `drifted` field:
- `quote`: the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range
- `anchor`: use it to locate the current position of the content; line numbers may be stale after edits
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best

Unresolved comments may have `replies` — read them before acting.

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

The finish prompt on stdout includes the command to run again — use it to start a new round.

On subsequent calls, `crit` automatically signals round-complete first, then blocks until the next "Finish Review" click.

Use the same shell strategy selected in Step 2:

- Background-capable shell: invoke the next `crit` round with `background: true` and no `timeout`, then end the response and wait for the automatic completion notification.
- Foreground-only shell: invoke the next `crit` round with `timeout: 86400000` and block until it completes.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, return to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.
