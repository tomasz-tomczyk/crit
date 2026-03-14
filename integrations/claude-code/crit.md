---
description: "Review code changes or a plan with crit inline comments"
allowed-tools: Bash(crit:*), Bash(command ls:*), Read, Edit, Glob
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Determine review mode

Choose what to review based on context:

1. **User argument** - if the user provided `$ARGUMENTS` (e.g., `/crit my-plan.md`), review that file
2. **Git changes** - if no argument, check for uncommitted changes:
   ```bash
   git status --porcelain 2>/dev/null | head -1
   ```
   If there are changes, run `crit` with no arguments (git mode) - it auto-detects changed files
3. **Find a plan** - if no changes, search for recent plan files:
   ```bash
   command ls -t ~/.claude/plans/*.md 2>/dev/null | grep -v -E '(-agent-)' | head -5
   ```
   Or search the working directory for plan-like `.md` files

Show the selected mode/file to the user and ask for confirmation.

## Step 2: Run crit for review

If a crit server is already running from earlier in this conversation, skip launching and run `crit go <port>` to trigger a new round instead.

Otherwise, run `crit` **in the background** using `run_in_background: true`:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
```

Tell the user: **"Crit is open in your browser. Leave inline comments on the plan, then click 'Finish Review'. Type 'go' here when you're done."**

Wait for the user to respond before proceeding.

## Step 3: Read the review output

After the user confirms, read the `.crit.json` file in the repo root (or working directory) using the Read tool.

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
3. Revise the **referenced file** to address the feedback - this could be the plan file or any code file from the git diff
4. Use the Edit tool to make targeted changes
5. Mark it resolved in `.crit.json`: set `"resolved": true`, optionally add `"resolution_note"` (what you did) and `"resolution_lines"` (where in the updated file, e.g. `"12-15"`)

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested and stop the background `crit` process.

## Step 5: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

The port is shown in crit's startup output (default: a random available port). This triggers a new review round in the browser with a diff of what changed.

## Step 6: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
