---
name: crit-review
description: Run the Crit review loop for plans or code changes
compatibility: opencode
---

## What I do

- Launch Crit for a plan file or the current git diff.
- Wait for the user to review changes in the browser.
- Read `.crit.json` and address unresolved inline comments.
- Signal the next review round with `crit go <port>` when edits are done.

## When to use me

Use this when the user asks to review a plan, spec, or code changes in Crit, or when project instructions require a Crit pass before accepting non-trivial changes.

## Workflow

1. Decide whether to review a specific file or the current git diff.
2. Run `crit <file>` for an explicit file, or `crit` for git mode.
3. Tell the user to leave inline comments in the browser, click Finish Review, and reply with `go` when they are done.
4. Read `.crit.json` and find comments where `resolved` is `false`.
5. Revise the referenced files to address each unresolved comment.
6. Mark comments resolved in `.crit.json` if the workflow calls for it.
7. Run `crit go <port>` to trigger the next review round.

## Guardrails

- Do not continue past the review step until the user confirms they are done.
- Treat `.crit.json` as the source of truth for line references and comment status.
- If there are no unresolved comments, tell the user no changes were requested and stop.
