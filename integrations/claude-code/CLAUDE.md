# Crit - Plan Review

Before implementing any non-trivial feature, write an implementation plan as a markdown file.

After writing the plan, launch Crit to open it for review:

```bash
crit $PLAN_FILE
```

Tell the user: "I've opened the plan in Crit for review. Leave inline comments, then click Finish Review. Type 'go' here when you're done."

Do NOT begin implementation until the user has reviewed and approved the plan.

After review, read the `.review.md` file to see the user's inline comments. Address each comment by revising the original plan file. The file change triggers Crit's live reload so the user can review again.

When `crit go <port>` is called (or the user says the plan is approved), proceed with implementation.
