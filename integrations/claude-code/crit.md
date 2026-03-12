---
description: "Review code changes or a plan with crit inline comments"
allowed-tools: Bash(crit:*), Bash(curl:*), Bash(command ls:*), Bash(pgrep:*), Read, Edit, Glob
---

# Review with Crit

Review and revise code changes or a plan using `crit` for inline comment review.

## Step 1: Check for existing crit instance

Before launching a new instance, check if crit is already running from a previous `/crit` invocation:

```bash
curl -s http://localhost:${CRIT_PORT:-0}/api/session 2>/dev/null
```

If you remember the port from a previous `/crit` run in this conversation AND the curl succeeds (returns JSON), skip to **Step 3a** — crit is already running. Call `crit go <port>` to trigger a new review round with inline diffs of your changes, then tell the user to review the new round.

If no existing instance is found, continue to Step 2.

## Step 2: Determine review mode

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

## Step 3: Run crit for review

Run `crit` **in the background** using `run_in_background: true`:

```bash
# For a specific file:
crit <plan-file>

# For git mode (no args):
crit
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

Identify all comments where `"resolved": false`.

## Step 5: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for (clarification, change, addition, removal)
2. If a comment contains a suggestion block, apply that specific change
3. Revise the **referenced file** to address the feedback - this could be the plan file or any code file from the git diff
4. Use the Edit tool to make targeted changes

Editing the plan file triggers Crit's live reload - the user sees changes in the browser immediately.

**If there are zero review comments**: inform the user no changes were requested and stop the background `crit` process.

## Step 6: Signal completion

After all comments are addressed, signal to crit that edits are done:

```bash
crit go <port>
```

The port is shown in crit's startup output (default: a random available port). This triggers a new review round in the browser with a diff of what changed.

## Step 7: Summary

Show a summary:
- Number of review comments found
- What was changed for each
- Any comments that need further discussion

Ask the user if they want another review pass or if the plan is approved for implementation.
