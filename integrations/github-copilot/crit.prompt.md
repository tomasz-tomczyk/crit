# Review Plan with Crit

Review and revise the current plan using `crit` for inline comment review.

## Step 1: Find the plan file

Determine which plan file to review:

1. If the user specified a file after the command, use that
2. Otherwise, search for `.md` files in the current directory that look like plans

Show the selected plan file to the user and ask for confirmation before proceeding.

## Step 2: Run crit for review

Run `crit` in a terminal:

```bash
crit <plan-file>
```

Tell the user: **"Crit is open in your browser. Leave inline comments on the plan, then click 'Finish Review'. Type 'go' here when you're done."**

Wait for the user to respond before proceeding.

## Step 3: Read the review output

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

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **original plan file** (not the review file) to address the feedback

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested.

## Step 5: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

The port is shown in crit's startup output. This triggers a new review round in the browser with a diff of what changed.

## Step 6: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
