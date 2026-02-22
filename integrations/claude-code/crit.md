---
description: "Review the current plan with crit inline comments"
allowed-tools: Bash(crit:*), Bash(command ls:*), Read, Edit, Glob
---

# Review Plan with Crit

Review and revise the current plan using `crit` for inline comment review.

## Step 1: Find the plan file

Determine which plan file to review, using this priority:

1. **User argument** - if the user provided `$ARGUMENTS` (e.g., `/crit my-plan.md`), use that file path
2. **Recent plans** - check for `.md` files in `~/.claude/plans/`, excluding `*-agent-*.md` and `*.review.md`:
   ```bash
   command ls -t ~/.claude/plans/*.md 2>/dev/null | grep -v -E '(-agent-|\.review\.md$)' | head -5
   ```
3. **Current directory** - search for plan-like `.md` files in the working directory

Show the selected plan file to the user and ask for confirmation before proceeding.

## Step 2: Run crit in background and wait for review

Run `crit` **in the background** using `run_in_background: true`:

```bash
crit <plan-file> --no-open
```

Parse the port from the startup output (e.g. `Listening on http://localhost:PORT`).

Then block until the reviewer finishes — run this as a regular Bash call (not background):

```bash
crit wait <port>
```

This blocks until the user clicks 'Finish Review', then prints the review prompt to stdout and exits. No need to tell the user to type 'go' — this is fully automatic.

## Step 3: Read the review output

After `crit wait` returns, read the review file at `<plan-file-stem>.review.md` using the Read tool.

Identify all `> **[REVIEW COMMENT` blocks. Each block contains feedback about the section above it.

## Step 4: Address each review comment

For each review comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block (indented original text with edits), apply that specific change
3. Revise the **original plan file** (not the review file) to address the feedback
4. Use the Edit tool to make targeted changes

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested and stop the background `crit` process.

## Step 5: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go --wait <port>
```

The port is shown in crit's startup output (default: a random available port). This signals a new review round in the browser (with a diff of what changed) and blocks until the reviewer clicks Finish — the prompt is printed to stdout automatically.

## Step 6: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
