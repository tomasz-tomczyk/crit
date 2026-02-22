# Crit - Plan Review

Before implementing any non-trivial feature, write an implementation plan as a markdown file.

After writing the plan, launch Crit in the background and wait for the review:

```bash
# Start crit in background (use run_in_background: true in Bash tool)
crit $PLAN_FILE --no-open

# Block until reviewer clicks Finish — prompt printed to stdout, then exits
crit wait <port>
```

Parse the port from crit's startup output (`Listening on http://localhost:PORT`). No user interaction needed — `crit wait` blocks automatically and prints the prompt when the review is done.

After `crit wait` returns, read the `.review.md` file to see the user's inline comments. Address each comment by revising the original plan file. The file change triggers Crit's live reload so the user can review again.

For subsequent rounds, signal completion and wait for the next review:

```bash
crit go --wait <port>
```

This signals round-complete and blocks until the reviewer clicks Finish again.
