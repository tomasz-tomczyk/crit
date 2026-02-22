# Review Plan with Crit

Review and revise the current plan using `crit` for inline comment review.

## Step 1: Find the plan file

Determine which plan file to review:

1. If the user specified a file after the command, use that
2. Otherwise, search for `.md` files in the current directory that look like plans (exclude `*.review.md`)

Show the selected plan file to the user and ask for confirmation before proceeding.

## Step 2: Run crit in background and wait for review

Start crit in the background and wait for the review automatically:

```bash
crit <plan-file> --no-open --port 3001 & crit wait 3001
```

This starts crit on port 3001, then blocks until the reviewer clicks 'Finish Review'. The review prompt is printed to stdout and the process exits — no need to tell the user to type 'go'.

## Step 3: Read the review output

After `crit wait` exits, read the review file at `<plan-file-stem>.review.md`.

Identify all `> **[REVIEW COMMENT` blocks. Each block contains feedback about the section above it.

## Step 4: Address each review comment

For each review comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block (indented original text with edits), apply that specific change
3. Revise the **original plan file** (not the review file) to address the feedback

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested.

## Step 5: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go --wait <port>
```

The port is shown in crit's startup output. This signals a new review round in the browser (with a diff of what changed) and blocks until the reviewer clicks Finish — the prompt is printed to stdout automatically.

## Step 6: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
