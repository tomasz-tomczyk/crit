# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Check for existing crit instance

Before launching a new instance, check if crit is already running from a previous `/crit` invocation. If you remember the port from a previous run in this conversation, try reaching the server:

```bash
curl -s http://localhost:<port>/api/session 2>/dev/null
```

If the curl succeeds (returns JSON), skip to **Step 3a** — crit is already running. Call `crit go <port>` to trigger a new review round with inline diffs of your changes, then tell the user to review the new round.

If no existing instance is found, continue to Step 2.

## Step 2: Determine review mode

Choose what to review based on context:

1. If the user specified a file after the command, use that
2. Otherwise, check for uncommitted git changes - if found, run `crit` with no arguments
3. If no changes, search for `.md` files in the current directory that look like plans

Show the selected mode/file to the user and ask for confirmation before proceeding.

## Step 3: Run crit for review

Run `crit` in a terminal:

```bash
crit <plan-file>
```

**Remember the port** from crit's startup output — you'll need it for `crit go` later and for detecting the running instance if `/crit` is called again.

Tell the user: **"Crit is open in your browser. Leave inline comments on the plan, then click 'Finish Review'. Type 'go' here when you're done."**

Wait for the user to respond before proceeding.

### Step 3a: Reuse existing crit instance

If crit was already running (detected in Step 1), trigger a new round:

```bash
crit go <port>
```

This opens a new review round in the browser showing a diff of changes since the last round. Tell the user: **"Crit has a new review round showing your changes. Leave inline comments, then click 'Finish Review'. Type 'go' here when you're done."**

Wait for the user to respond before proceeding.

## Step 4: Read the review output

After the user confirms, read the `.crit.json` file in the repo root (or working directory).

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

Identify all comments where `"resolved": false`.

## Step 5: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **referenced file** to address the feedback - this could be the plan file or any code file

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested.

## Step 6: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

The port is shown in crit's startup output. This triggers a new review round in the browser with a diff of what changed.

## Step 7: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
