# Crit - Review Agent Output

Before accepting any non-trivial changes, review them with Crit.

Two workflows:

**Reviewing a plan** - after writing an implementation plan - launch Crit to open it for review:

```bash
crit $PLAN_FILE
```

**Reviewing code changes** - after writing code, launch Crit to review all changed files:

```bash
crit
```

Tell the user: "I've opened your changes in Crit for review. Leave inline comments, then click Finish Review. Type 'go' here when you're done."

Do NOT continue until the user has reviewed.

After review, read `.crit.json` to see the user's inline comments. Each comment has `start_line`, `end_line`, `body`, and `resolved` fields. Address each unresolved comment by revising the referenced file. When done, run `crit go <port>` to trigger a new round.
