---
name: crit
description: "Review code changes, a plan, a live page (running dev server), or a local HTML file with Crit inline comments and structured human feedback. Use only when the user explicitly invokes /crit or directly asks to use Crit; a generic review request does not count."
---

# Review with Crit

This is the interactive, human-in-the-browser review cycle. Run it only after
the user explicitly invokes `/crit` or directly asks to use Crit. A generic
request to review code, a plan, a diff, a PR, or a page does not count.

Review and revise code changes, plans, live pages (running dev servers, staging URLs), or local HTML files using `crit` for inline comment review.

## Step 1: Pass arguments to `crit`

The CLI auto-detects the review mode from its arguments. **Do not ask the user which mode to use.** Pass arguments through:

```
crit <arguments>               # file, dir, URL, .html — CLI auto-detects mode
crit --pr <num|url>            # GitHub PR (range mode)
crit --range <base>..<head>    # commit range (range mode)
crit                           # no args → branch diff
```
If no arguments, check conversation context:

1. A plan file was written earlier in this conversation → `crit <plan-file>`
2. Otherwise → bare `crit` (branch diff)

If the user wants to review remotely — e.g. from a phone via Tailscale, or any URL other than localhost:

Run crit with the public URL and network flags:

```bash
crit --public-url "https://<tailscale-magicdns-name-or-hostname>" --allow-unauthenticated-network --no-open <file>
```

- `--public-url` only changes the URL crit prints/validates against — it does **not** expose crit to the network by itself.
- `--allow-unauthenticated-network` is required for any non-loopback exposure. crit has no auth: anyone who can reach the URL can read the repo and post comments that may trigger agents. Confirm the user actually wants that blast radius before using it.
- **Do not bind `--host` to a tailscale/LAN IP directly** — the recommended shape is crit staying on loopback while `tailscale serve --bg --https=443 http://127.0.0.1:<port>` (or an SSH tunnel) does the reverse proxy. Get `<port>` from crit's own startup output, or pass a fixed one with `-p`.

## Step 2: Launch crit and block until review completes

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

Run `crit` in the foreground and block until it exits:

```bash
crit <plan-file>   # specific file
crit               # git mode
```

If a crit server is already running from earlier in this conversation, `crit` automatically connects to it. Starting from scratch, it spawns the daemon, opens the browser, and blocks until the user clicks "Finish Review".

`crit` prints the review URL on startup (e.g. `Started crit daemon at http://localhost:<port>`). Relay it verbatim:

> **"Crit is open at http://localhost:<port>. Leave inline comments, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read the review file early. Wait for the foreground command to finish — that is how you know the human is done reviewing.

## Step 3: Read the review output

When `crit` completes, read **stdout** and follow its instructions. Check **stderr** for `approved: true` or `approved: false`.

When a comment has `quote`, `anchor`, or `drifted`:
- `quote`: the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range
- `anchor`: use it to locate the current position of the content; line numbers may be stale after edits
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best

**Fallback** (mid-round re-entry, plan hooks, or headless workflows): `crit comments` / `crit comments --json`. Use `crit comments --plan <slug>` for plan-mode reviews.

## Step 4: Address each review comment

For each unresolved comment:

1. Understand what the comment asks for
2. If it contains a suggestion block, apply that specific change
3. Revise the referenced file (plan or code file from the diff)
4. Reply with what you did: `crit comment --reply-to <id> --author 'Hermes' '<what you did>'` (reply bodies support markdown)
5. **Do not pass `--resolve`.** Resolving is the reviewer's call. Only add `--resolve` if the user explicitly asks.

Editing the plan file triggers Crit's live reload — the user sees changes in the browser immediately.

### When replying to multiple comments

Use `--json` for a single bulk call instead of one invocation per comment:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Hermes'
```

## Step 5: Signal completion and start next round

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

The finish prompt on stdout includes the command to run again — use it to start a new round.

On subsequent calls, `crit` automatically signals round-complete first, then blocks until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, return to Step 3. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## Sharing

If the user asks for a URL, a shareable link, or to share the review:

```bash
crit share <file>
```

**Always relay the full output to the user** — copy the URL directly into your response. Don't make them dig through tool output.

To remove a shared review:

```bash
crit unpublish [file...]
```

### QR codes

Only use `--qr` in real terminal environments with monospace rendering. Skip it in mobile apps or web chat UIs — Unicode block characters won't render.

```bash
crit share --qr <file>
```
