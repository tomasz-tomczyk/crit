# Crit - Review Agent Output

Before implementing any non-trivial feature, write an implementation plan as a markdown file.

## Writing plans

When asked to implement a feature, first create a plan file that covers:
- What will be built
- Which files will be created or modified
- Key design decisions and trade-offs
- Step-by-step implementation order

## Review with Crit

After writing a plan or code, launch Crit to open it for review:

```bash
# Review a specific file (plan, spec, etc.)
crit $PLAN_FILE

# Review all changed files in the repo
crit
```

If background tasks are supported, run `crit listen <port>` in the background to be notified automatically when the user clicks Finish Review — do NOT ask the user to type anything.

Otherwise, tell the user: "I've opened your changes in Crit for review. Leave inline comments, then click Finish Review. Let me know when you're done."

Do NOT begin implementation until the review is complete.

## After review

Read `.crit.json` to find the user's inline comments. Comments are grouped per file with `start_line`/`end_line` referencing the source. A comment is unresolved if `"resolved": false` or if the `resolved` field is missing. Address each unresolved comment by revising the referenced file. After addressing, set `"resolved": true` and optionally `"resolution_note"` and `"resolution_lines"`. When done, run `crit go <port>` to trigger a new round.

Only proceed after the user approves.

## Leaving comments programmatically

Use `crit comment` to add inline review comments to `.crit.json` without opening the browser:

```bash
crit comment <path>:<line> '<body>'
crit comment <path>:<start>-<end> '<body>'
crit comment --author 'Aider' src/auth.go:42 'Missing null check here'
```

Paths are relative, line numbers are 1-indexed, comments are appended (never replaced). Creates `.crit.json` automatically if it doesn't exist.

## GitHub PR Integration

```bash
crit pull [pr-number]              # Fetch PR comments into .crit.json
crit push [--dry-run] [pr-number]  # Post .crit.json comments as PR review
```

Requires `gh` CLI. PR number auto-detected from current branch.
