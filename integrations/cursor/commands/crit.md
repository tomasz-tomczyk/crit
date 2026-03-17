# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Determine review mode

Choose what to review based on context:

1. If the user specified a file after the command, use that
2. If no argument, check if a plan was written earlier in this conversation. If so, review that file
3. Otherwise, run `crit` with no arguments — it auto-detects what to review: uncommitted changes, or all changes on the current branch vs the default branch. Works on clean branches too.

Don't ask for confirmation — just proceed with whichever mode applies.

## Step 2: Launch crit server

If a crit server is already running from earlier in this conversation, skip to Step 3 and run `crit go <port>` there instead.

Run `crit` in a terminal:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
```

Note the port from crit's startup output.

## Step 3: Block until review completes

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

If background tasks are supported, run `crit listen <port>` in the background:

```bash
crit listen <port>
```

Tell the user: **"Crit is open in your browser. Leave inline comments, then click Finish Review."**

**Do NOT proceed until `crit listen` completes.** Do NOT ask the user to type anything. Do NOT read `.crit.json` early. Wait for the background task to finish — that is how you know the human is done reviewing.

**Fallback:** If background tasks are NOT supported, tell the user: **"Type 'go' here when you're done."** and wait for the user to respond.

## Step 4: Read the review output

Read the `.crit.json` file in the repo root (or working directory).

The file contains structured JSON with comments per file:

```json
{
  "files": {
    "plan.md": {
      "comments": [
        { "id": "c1", "start_line": 5, "end_line": 10, "body": "Clarify this step", "quote": "specific words", "resolved": false }
      ]
    }
  }
}
```

Identify all comments where `"resolved": false` or where the `resolved` field is missing (missing means unresolved). If a comment has a `"quote"` field, it contains the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range.

## Step 5: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **referenced file** to address the feedback - this could be the plan file or any code file
4. Reply to the comment with what you did: `crit comment --reply-to <id> --resolve '<what you did>'`

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested.

## Step 6: Signal completion and start next round

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

This triggers a new review round in the browser with a diff of what changed.

**CRITICAL — immediately after `crit go`, you MUST run `crit listen <port>` again.** This is the same as Step 3. Do NOT skip it.

```bash
crit listen <port>
```

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit listen` completes.** When it does, go back to Step 4. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

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
