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

**CRITICAL — you MUST run `crit listen <port>` after launching crit.** Run it in the background if supported:

```bash
crit listen <port>
```

**Do NOT proceed until `crit listen` completes.** Do NOT ask the user to type anything. Do NOT read `.crit.json` early. `crit listen` blocks until the user clicks Finish Review — that is how you know they are done.

**Fallback:** If background tasks are NOT supported, tell the user: "Leave inline comments, then click Finish Review. Let me know when you're done." and wait for a response.

## After review

Read `.crit.json` to find the user's inline comments. Comments are grouped per file with `start_line`/`end_line` referencing the source. A comment is unresolved if `"resolved": false` or if the `resolved` field is missing. Address each unresolved comment by revising the referenced file. After addressing, reply with what you did: `crit comment --reply-to <id> --resolve '<what you did>'`.

When done, run `crit go <port>` to trigger a new round, then **immediately run `crit listen <port>` again** to wait for the next review. Do NOT skip `crit listen` between rounds.

Only proceed after the user approves (finishes a round with zero comments).

## Leaving comments programmatically

Use `crit comment` to add inline review comments to `.crit.json` without opening the browser:

```bash
crit comment <path>:<line> '<body>'
crit comment <path>:<start>-<end> '<body>'
crit comment --author 'Windsurf' src/auth.go:42 'Missing null check here'
crit comment --reply-to c1 --resolve --author 'Windsurf' 'Added null check'
```

Paths are relative, line numbers are 1-indexed, comments are appended (never replaced). Creates `.crit.json` automatically if it doesn't exist.

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
crit share --share-url https://crit.live <file>  # Explicit share URL
```

Rules:
- **No server needed** — `crit share` reads files directly from disk
- **`--qr` is terminal-only** — only use when the user has a real terminal with monospace font rendering. Do not use in mobile apps (e.g. Claude Code mobile), web chat UIs, or any environment where Unicode block characters won't render correctly
- **Comments included** — if `.crit.json` exists, comments for the shared files are included automatically
- **Relay the output** — always copy the URL (and QR code if `--qr` was used) from the command output and include it directly in your response to the user. Do not make them dig through tool output
- **State persisted** — share URL and delete token are saved to `.crit.json`
- **Unpublish reads `.crit.json`** — uses the stored delete token to remove the review

## GitHub PR Integration

```bash
crit pull [pr-number]              # Fetch PR comments into .crit.json
crit push [--dry-run] [pr-number]  # Post .crit.json comments as PR review
```

Requires `gh` CLI. PR number auto-detected from current branch.
