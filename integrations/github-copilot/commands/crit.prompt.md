# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Determine review mode

Choose what to review based on context:

1. If the user specified a file after the command, use that
2. If no argument, check if a plan was written earlier in this conversation. If so, review that file
3. Otherwise, run `crit` with no arguments — it auto-detects what to review: uncommitted changes, or all changes on the current branch vs the default branch. Works on clean branches too.

Don't ask for confirmation — just proceed with whichever mode applies.

## Step 2: Run crit for review

If a crit server is already running from earlier in this conversation, skip launching and run `crit go <port>` to trigger a new round instead. Then skip to Step 2b.

Run `crit` in a terminal:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
```

Note the port from crit's startup output.

### Step 2b: Listen for review completion

If background tasks are supported, run `crit listen <port>` in the background and wait for it to complete — do NOT ask the user to type anything.

Otherwise, tell the user: **"Crit is open in your browser. Leave inline comments on the plan, then click 'Finish Review'. Type 'go' here when you're done."** and wait for the user to respond.

## Step 3: Read the review output

Read the `.crit.json` file in the repo root (or working directory).

The file contains structured JSON with comments per file:

```json
{
  "files": {
    "plan.md": {
      "comments": [
        { "id": "c1", "start_line": 5, "end_line": 10, "body": "Clarify this step", "resolved": false }
      ]
    }
  }
}
```

Identify all comments where `"resolved": false` or where the `resolved` field is missing (missing means unresolved).

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **referenced file** to address the feedback - this could be the plan file or any code file
4. Mark it resolved in `.crit.json`: set `"resolved": true`, optionally add `"resolution_note"` (what you did) and `"resolution_lines"` (where in the updated file, e.g. `"12-15"`)

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested.

## Step 5: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

The port is shown in crit's startup output. This triggers a new review round in the browser with a diff of what changed.

## Step 6: Next round

After `crit go <port>` triggers a new round, listen for the next review completion (same as Step 2b).

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

If the user finishes with zero comments, the review is approved — stop the loop and proceed.
