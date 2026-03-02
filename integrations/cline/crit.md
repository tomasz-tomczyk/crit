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

Tell the user: "I've opened your changes in Crit for review. Leave inline comments, then click Finish Review. Let me know when you're done."

Do NOT begin implementation until the user confirms the review is approved.

## After review

Read `.crit.json` to find the user's inline comments. Each file's comments are in a structured JSON format with `start_line`, `end_line`, `body`, and `resolved` fields. Address each unresolved comment by revising the referenced file.

Only proceed after the user approves.
