# Crit - Plan Review Workflow

Before implementing any non-trivial feature, write an implementation plan as a markdown file.

## Writing plans

When asked to implement a feature, first create a plan file that covers:
- What will be built
- Which files will be created or modified
- Key design decisions and trade-offs
- Step-by-step implementation order

## Review with Crit

After writing the plan, launch Crit to open it for review:

```bash
crit $PLAN_FILE
```

Tell the user: "I've opened the plan in Crit for review. Leave inline comments, then click Finish Review. Let me know when you're done."

Do NOT begin implementation until the user confirms the plan is approved.

## After review

Read `.crit.json` to find the user's inline comments. Each file's comments are in a structured JSON format with `start_line`, `end_line`, `body`, and `resolved` fields. Address each unresolved comment by revising the original plan file.

Only proceed with implementation after the user approves the final plan.
