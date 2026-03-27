---
name: crit
description: Review code changes or a plan with crit inline comments. Use when asked to review code, a plan, a diff, or when you want structured human feedback on your work.
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Determine review mode

Choose what to review based on context:

1. **User argument** — if the user specified a file, review it with `crit <file>`
2. **Recent plan** — if you just wrote a plan, review that file with `crit <plan-file>`
3. **Branch review** — otherwise, run `crit` with no arguments. It auto-detects what to review: uncommitted changes, or all changes on the current branch vs the default branch. Works on clean branches too.

Don't ask for confirmation — just proceed with whichever mode applies.

## Step 2: Launch crit and block until review completes

Run `crit` to open the review UI in the browser:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
```

This starts the daemon if needed (or connects to an existing one), opens the browser, and blocks until the user clicks "Finish Review". Feedback is printed to stdout when it exits.

Tell the user: **"Crit is open in your browser. Leave inline comments, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read `.crit.json` early. Wait for crit to exit — that is how you know the human is done reviewing.

## Step 3: Read the review output

When `crit` completes, read the `.crit.json` file in the repo root (or working directory).

The file contains structured JSON with comments per file and review-level comments:

```json
{
  "review_comments": [
    { "id": "r0", "body": "Overall feedback", "resolved": false }
  ],
  "files": {
    "plan.md": {
      "comments": [
        { "id": "c1", "start_line": 5, "end_line": 10, "body": "Clarify this step", "quote": "specific words", "resolved": false },
        { "id": "c2", "body": "File needs restructuring", "resolved": false }
      ]
    }
  }
}
```

There are three types of comments: `review_comments` (general feedback, `r`-prefixed IDs), file comments (in per-file `comments` array with no `start_line`/`end_line`), and line comments (with `start_line`/`end_line`). If a comment has lines, it's about those lines. If not, it's about the file as a whole.

Identify all comments where `"resolved": false` or where the `resolved` field is missing (missing means unresolved). If a comment has a `"quote"` field, it contains the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range.

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **referenced file** to address the feedback — this could be the plan file or any code file from the git diff
4. Reply to the comment with what you did: `crit comment --reply-to <id> --author 'Codex' '<what you did>'`

When addressing multiple comments, use `--json` to reply to them all in one call:

```bash
echo '[
  {"reply_to": "c1", "body": "Fixed"},
  {"reply_to": "c2", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Codex'
```

Editing the plan file triggers Crit's live reload — the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested and stop.

## Step 5: Signal completion and start next round

Run the **exact same `crit` command from Step 2** again. This is critical — if you launched `crit plan.md` in Step 2, you must run `crit plan.md` again here (not bare `crit`). The daemon is keyed by the arguments, so mismatched args will start a new daemon instead of reconnecting.

```bash
# Must match Step 2 exactly:
crit <same-args-as-step-2>
```

On subsequent calls, `crit` automatically signals round-complete first, then blocks again until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, go back to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## Sharing

If the user asks for a URL, a link, or to share the review, run:

```bash
crit share <file>
```

Always relay the full output to the user — copy the URL from the command output and include it directly in your response.

To remove a shared review:

```bash
crit unpublish
```
