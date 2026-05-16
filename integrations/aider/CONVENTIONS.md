# Crit - Review Agent Output

Before implementing any non-trivial feature, write an implementation plan as a markdown file.

## Writing plans

When asked to implement a feature, first create a plan file that covers:
- What will be built
- Which files will be created or modified
- Key design decisions and trade-offs
- Step-by-step implementation order

## Review with Crit

After writing a plan or code, launch Crit:

```bash
crit $PLAN_FILE                       # Review a specific file
crit                                  # Review all changed files in the repo
crit --pr <num|url>                   # Review a GitHub PR (range mode)
crit --range <baseSHA>..<headSHA>     # Review a commit range (range mode)
```

**CRITICAL — you MUST run `crit` and block until it completes.**

`crit` starts the daemon if needed, opens the browser, and blocks until the user clicks "Finish Review". It prints the review URL on startup (e.g. `Started crit daemon at http://localhost:<port>`) — relay that URL verbatim.

- Do NOT proceed until `crit` completes.
- Do NOT ask the user to type anything.
- Do NOT read the review file early.

## After review

`crit` stdout includes the review file path. Read it. Three comment scopes:

- **Line comments** — in `files.<path>.comments` with `start_line`/`end_line`
- **File comments** — same array, `scope: "file"`, lines are 0
- **Review comments** — in the top-level `review_comments` array, `scope: "review"`

Address each comment where `resolved` is `false` or missing.

Field guidance:
- `quote`: the specific text the reviewer selected — focus changes on the quoted text rather than the whole range.
- `anchor`: full text of the commented lines when placed — locate content by anchor, line numbers may be stale.
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best.

For each unresolved comment:
1. Revise the referenced file using your edit tools.
2. Reply with what you did: `crit comment --reply-to <id> --author 'Aider' '<what you did>'` (markdown supported).
3. **Never pass `--resolve`** unless the user explicitly asks. Resolving is the reviewer's call.

When replying to multiple comments, use `--json`:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Aider'
```

For multi-paragraph reply bodies, prefer `--file <path>`. Raw newlines inside JSON strings are invalid, and shell heredocs make it easy to slip one in:

```bash
cat > /tmp/replies.json <<'EOF'
[
  {"reply_to": "c_a1b2c3", "body": "Fixed.\n\nDetails: split helper, added null guard."}
]
EOF
crit comment --json --file /tmp/replies.json --author 'Aider'
```

`--file -` reads stdin (same as the default).

## Next round

When done, run the command crit printed after `Next round:` in its previous output. The daemon is keyed by arguments, so this matters — `crit plan.md` and bare `crit` are different sessions.

`crit` automatically signals round-complete, then blocks until the next "Finish Review" click. Only proceed after the user approves (a round finishes with zero comments).

## CLI Reference

### `crit comment`

```bash
crit comment --author 'Aider' '<body>'                       # Review-level
crit comment --author 'Aider' <path> '<body>'                # File-level
crit comment --author 'Aider' <path>:<line> '<body>'         # Line
crit comment --author 'Aider' <path>:<start>-<end> '<body>'  # Line range
crit comment --reply-to <id> --author 'Aider' '<body>'       # Reply (c_… or r_…)
```

Hard rules:
- Always pass `--author 'Aider'`.
- Always single-quote the body — double quotes break on backticks and shell metachars.
- Line numbers reference the file on disk (1-indexed), not diff line numbers.
- Reply bodies support markdown.
- Only pass `--resolve` when the user explicitly asks.

If `crit comment` errors with "comment found in multiple files", disambiguate with `--path src/foo.go`.

### Bulk `--json`

For 3+ comments, prefer `--json` (atomic, single write). Synopsis:

```
crit comment --json [--file <path>] [--author <name>]
```

Stdin form (short single-line bodies):

```bash
echo '[
  {"body": "overall feedback", "scope": "review"},
  {"path": "session.go", "body": "restructure", "scope": "file"},
  {"file": "src/auth.go", "line": 42, "body": "Missing null check"},
  {"file": "src/auth.go", "line": "50-55", "body": "Extract to helper"},
  {"reply_to": "c_a1b2c3", "body": "Fixed — added null check"}
]' | crit comment --json --author 'Aider'
```

`--file <path>` form — preferred whenever a body has paragraph breaks (raw newlines in a JSON string are invalid):

```bash
cat > /tmp/crit-bulk.json <<'EOF'
[
  {"file": "src/auth.go", "line": 42, "body": "Para 1.\n\nPara 2."}
]
EOF
crit comment --json --file /tmp/crit-bulk.json --author 'Aider'
```

Scope inference: `reply_to` → reply; no `file`/`line` → review; `path` only → file; `path` + `line` → line.

### Sharing

```bash
crit share <file> [file...]                          # Upload and print URL
crit share --qr <file>                               # Also print QR code (terminal only)
crit share --org <slug> <file>                       # Share under an organization
crit share --org <slug> --visibility unlisted <file> # Org share with explicit visibility
crit unpublish                                       # Remove shared review
```

Always relay the full output (URL, QR) directly in your response — don't make the user dig through tool output.
- **`--org <slug>`** shares under an organization. Visibility defaults to `organization` (members only). Override with `--visibility` (`organization`, `unlisted`, `public`).

### GitHub PR sync

```bash
crit pull [pr-number]                                    # Fetch PR comments
crit push [--dry-run] [--event <type>] [-m <msg>] [pr]   # Post review as PR review
```

Requires `gh` CLI. `--event`: `comment` (default), `approve`, `request-changes`.
