---
description: "Review the current Claude plan with crit inline comments"
allowed-tools: Bash(crit:*), Bash(command ls:*), Read, Edit, Glob
---

# Review Plan with Crit

Review and revise the current plan using `crit` for inline comment review.

## Step 1: Find the plan file

Determine which plan file to review, using this priority:

1. **Session context** — if Claude is currently working with a plan file (e.g., in plan mode), use that
2. **User argument** — if the user provided `$ARGUMENTS` (e.g., `/review-plan my-plan.md`), use that file path
3. **Recent plans** — fall back to the most recently modified `.md` file in `~/.claude/plans/`, excluding files matching `*-agent-*.md` and `*.review.md`:
   ```bash
   command ls -t ~/.claude/plans/*.md | grep -v -E '(-agent-|\.review\.md$)' | head -5
   ```
4. **Current directory** — search for `.md` files in the working directory

Show the selected plan file to the user and ask for confirmation before proceeding.

## Step 2: Run crit for review

Run `crit` **in the background** using `run_in_background: true`:

```bash
crit <plan-file>
```

Then ask the user: **"Type 'go' when you're done reviewing in the browser."**

Wait for the user to respond before proceeding.

## Step 3: Read the review output

After the user confirms, use the **Read tool** (not bash) to read the review file at `<plan-file-without-.md>.review.md`.

Do NOT use `ls` or other bash commands to check for the file — just read it directly with the Read tool.

Identify all `> **[REVIEW COMMENT` blocks. Each block contains feedback from the user about the section above it.

## Step 4: Address each review comment

For each review comment found:

1. Understand what the comment is asking for (clarification, change, addition, removal)
2. Revise the **original plan file** (not the review file) to address the feedback
3. Use the Edit tool to make targeted changes to the plan

If a comment requires research or investigation before updating the plan, do that first.

Editing the plan file will automatically unblock the background `crit` process (it watches for file changes).

**If there are zero review comments**: inform the user no changes were requested. Use `TaskStop` to kill the background `crit` process (no need to touch the file).

## Step 5: Summary

After all comments are addressed, show a summary:

- Number of review comments found
- What was changed for each comment
- Any comments that need further discussion

Ask the user if they want to re-run crit for another review pass.
