---
description: Review code changes or a plan with crit inline comments
agent: build
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

If the `crit` skill is available, load it first.

## Step 1: Determine review mode

Choose what to review based on context:

1. If the user provided `$ARGUMENTS` (for example, `/crit plan.md`), review that file.
2. If no argument, check if a plan was written earlier in this conversation. If so, review that file.
3. Otherwise, run `crit` with no arguments — it auto-detects what to review: uncommitted changes, or all changes on the current branch vs the default branch. Works on clean branches too.

Don't ask for confirmation — just proceed with whichever mode applies.

## Step 2: Launch crit and block until review completes

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

If a crit server is already running from earlier in this conversation, `crit` will automatically connect to it — no need to track ports or skip steps.

Run `crit` in the foreground and block until it exits:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
```

This starts the daemon if needed (or connects to an existing one), opens the browser, and blocks until the user clicks "Finish Review". Feedback is printed to stdout when it exits.

Tell the user: **"Crit is open in your browser. Leave inline comments, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read the review file early. Wait for the foreground command to finish — that is how you know the human is done reviewing.

## Step 3: Read the review output

When `crit` completes, its stdout output includes the path to the review file. Read that file.

The file contains structured JSON with comments per file:

```json
{
  "files": {
    "plan.md": {
      "comments": [
        { "id": "c_a1b2c3", "start_line": 5, "end_line": 10, "body": "Clarify this step", "quote": "specific words", "anchor": "The sessions table needs a complete rewrite...", "resolved": false }
      ]
    }
  }
}
```

Identify all comments where `"resolved": false` or where the `resolved` field is missing (missing means unresolved). If a comment has a `"quote"` field, it contains the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range. If a comment has an `"anchor"` field, use it to locate the current position of the content rather than trusting `start_line`/`end_line` which may be stale after edits. If `"drifted": true`, the original content was removed or heavily rewritten — the line numbers are approximate at best.

Before acting on a comment, check its `replies` array — if you have already replied, the reviewer may be following up conversationally rather than requesting a new code change.

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for.
2. If a comment contains a suggestion block, apply that specific change.
3. Revise the referenced file to address the feedback - this could be the plan file or any code file from the git diff.
4. Reply to the comment with what you did: `crit comment --reply-to <id> --author 'OpenCode' '<what you did>'` (works for both file comment IDs like `c_a1b2c3` and review comment IDs like `r_f1e2d3`)

When addressing multiple comments, use `--json` to reply to them all in one call:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'OpenCode'
```

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

If there are zero review comments, inform the user that no changes were requested.

## Step 5: Signal completion and start next round

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

Run the **exact same `crit` command from Step 2** in the foreground and block until it exits. This is critical — if you launched `crit plan.md` in Step 2, you must run `crit plan.md` again here (not bare `crit`). The daemon is keyed by the arguments, so mismatched args will start a new daemon instead of reconnecting.

```bash
# Must match Step 2 exactly:
crit <same-args-as-step-2>
```

On subsequent calls, `crit` automatically signals round-complete first, then blocks again until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, go back to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## Sharing

If the user asks for a URL, a link, to share the review, or to show a QR code, run:

```bash
crit share <file>
```

**Always relay the full output to the user** — copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response. Do not make them dig through tool output to find it.

To also show a QR code — **only in real terminal environments** with monospace font rendering (not mobile apps like Claude Code mobile, or web chat UIs where Unicode block characters won't render):

```bash
crit share --qr <file>
```

To remove a shared review:

```bash
crit unpublish
```
