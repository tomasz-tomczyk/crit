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

**CRITICAL — you MUST run `crit` and block until it completes.**

Run `crit` (it starts the daemon if needed, opens the browser, and blocks until the user clicks "Finish Review"):

```bash
crit
```

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read the review file early. `crit` blocks until the user clicks Finish Review — that is how you know they are done.

## After review

The crit stdout output includes the review file path. Read that file to find the user's inline comments. Comments have three scopes: line comments in `files.<path>.comments` (with `start_line`/`end_line`), file comments (same array, `scope: "file"`, lines are 0), and review comments in the top-level `review_comments` array (`scope: "review"`, not tied to any file). Line comments include an `anchor` field containing the full text of the commented lines when the comment was placed — use this to locate the current position of the content rather than trusting `start_line`/`end_line` which may be stale after edits. If `drifted: true`, the original content was removed or heavily rewritten and line numbers are approximate. Address each unresolved comment by revising the referenced file. After addressing, reply with what you did: `crit comment --reply-to <id> --author 'Cline' '<what you did>'`. This works for both file comment IDs (e.g. `c_a1b2c3`) and review comment IDs (e.g. `r_f1e2d3`).

When addressing multiple comments, use `--json` to reply to them all in one call:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Cline'
```

When done, run the **exact same `crit` command from earlier** to signal round-complete and wait for the next review. If you launched `crit plan.md`, run `crit plan.md` again (not bare `crit`). The daemon is keyed by the arguments, so mismatched args will start a new daemon instead of reconnecting. On subsequent calls, `crit` automatically signals round-complete first, then blocks again until the next "Finish Review" click.

Only proceed after the user approves (finishes a round with zero comments).

## Leaving comments programmatically

Use `crit comment` to add review comments to the review file without opening the browser:

```bash
crit comment --author 'Cline' '<body>'                          # Review-level comment
crit comment --author 'Cline' <path> '<body>'                   # File-level comment
crit comment --author 'Cline' <path>:<line> '<body>'            # Line comment
crit comment --author 'Cline' <path>:<start>-<end> '<body>'     # Line range comment
crit comment --reply-to c_a1b2c3 --author 'Cline' '<body>'  # Reply to file comment
crit comment --reply-to r_f1e2d3 --author 'Cline' '<body>'  # Reply to review comment
```

Paths are relative, line numbers are 1-indexed, comments are appended (never replaced). Creates the review file automatically if it doesn't exist.

## Sharing Reviews

If the user asks for a URL, a link, to share their review, or to show a QR code, use `crit share`:

```bash
crit share <file> [file...]   # Upload and print URL
crit share --qr <file>        # Also print QR code (terminal only)
crit unpublish                # Remove shared review
```

Examples:

```bash
crit share <file>                                # Share a single file
crit share <file1> <file2>                       # Share multiple files
crit share --share-url https://crit.md <file>  # Explicit share URL
```

Rules:
- **No server needed** — `crit share` reads files directly from disk
- **`--qr` is terminal-only** — only use when the user has a real terminal with monospace font rendering. Do not use in mobile apps (e.g. Claude Code mobile), web chat UIs, or any environment where Unicode block characters won't render correctly
- **Comments included** — if the review file exists, comments for the shared files are included automatically
- **Relay the output** — always copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response to the user. Do not make them dig through tool output
- **State persisted** — share URL and delete token are saved to the review file
- **Unpublish reads the review file** — uses the stored delete token to remove the review

## GitHub PR Integration

```bash
crit pull [pr-number]                                    # Fetch PR comments into the review file
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]  # Post review comments as PR review
```

Requires `gh` CLI. PR number auto-detected from current branch. Event types for `--event`: `comment` (default), `approve`, `request-changes`.
