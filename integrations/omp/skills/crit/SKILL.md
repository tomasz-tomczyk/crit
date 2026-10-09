---
name: crit
description: "Review code changes, a plan, a live page (running dev server), or a local HTML file with Crit inline comments and structured human feedback. Use only when the user explicitly invokes /crit or directly asks to use Crit; a generic review request does not count."
---

# Review with Crit

Run this interactive browser review cycle only when the user invokes
`/skill:crit` or directly asks to use Crit. A generic request to review code,
a plan, a diff, a PR, or a page does not count.

## 1. Choose the target

Pass the user's arguments through. Crit auto-detects files, directories,
live URLs, and local HTML previews; do not ask which mode to use.

```bash
crit <arguments>
crit --pr <number-or-url>
crit --mr <number-or-url>
crit --range <base>..<head>
crit
```

Without arguments, review the plan file written earlier in this conversation,
if there is one; otherwise run bare `crit` for the branch diff.

## 2. Start the review and await completion

Crit opens the browser and stays running until the human clicks Finish Review.
Use omp's `bash` tool to launch the command chosen in Step 1 as an asynchronous
job with no deadline:

```json
{"command":"crit","async":true,"timeout":0}
```

Save the returned job ID. `timeout: 0` disables the default five-minute
command deadline; `async: true` alone does not. If asynchronous jobs are
unavailable, run the same command in the foreground with `timeout: 0`.
Do not use a named service: each review round is a finite command that exits
when feedback is ready.

Relay the printed review URL verbatim: "Crit is open at <URL>. Leave inline
comments, then click Finish Review." If the initial job result omits the URL,
read `proc://<job-id>` once for startup output.

Use `wait` when no independent work remains. Completion also auto-delivers.
If a wait returns a running snapshot or an unrelated message, keep waiting
for this job's completion; neither means the human finished reviewing.
Do not poll process output or the review file, hold an eval cell open,
launch a second Crit command, or ask the user to type a reply to signal
completion. An existing daemon is reused automatically.

## 3. Read feedback

After the command exits, read its output and follow the finish prompt.
Check for `approved: true` or `approved: false`; omp merges stdout and stderr.
Retrieve omitted output through `proc://<job-id>` or the output's
`artifact://` link. If approved, stop the review loop.
A command failure is not approval; report the error rather than proceeding.

For mid-round re-entry or headless workflows, use `crit comments --json`
(or `crit comments --plan <slug>` for plan reviews).

Review-level comments matter as well as file comments. Read existing replies.
Treat missing `resolved` as unresolved. Use `quote` to narrow the requested
change, `anchor` to locate content after edits, and `drifted: true` as a sign
that line numbers are approximate.

## 4. Address comments

Revise the referenced plan or source files and apply suggestion blocks when
present. Reply with what changed:

```bash
crit comment --reply-to <id> --author 'omp' '<what changed>'
crit comment --plan <slug> --reply-to <id> --author 'omp' '<what changed>'
```

Do not pass `--resolve` unless the user explicitly asks; resolution belongs
to the reviewer. For multiple replies, write JSON with the `write` tool:

```bash
crit comment --json --file /tmp/crit-replies.json --author 'omp'
```

Each entry has `reply_to` and `body`. Use the `crit-cli` skill for the full
schema and session disambiguation. Plan edits reload live in the browser.

## 5. Start the next round

Run the next-round command printed by Crit using the same asynchronous
`bash` pattern with `timeout: 0`. It signals round-complete and then waits
for the next Finish Review.

Tell the user only: "Replied in Crit. Review the changes in your browser and
click Finish Review when ready." Do not repeat your replies or list the
comments in chat. The reviewer reads them in Crit. Await command completion, then return to Step 3.
Continue until the reviewer approves.

## Sharing

Only share or unpublish when the user asks:

```bash
crit share <file> [file...]
crit unpublish [file...]
```

Relay the full printed URL. Use `--qr` only in real monospace terminals.
For another device, keep Crit on loopback and use an SSH tunnel or reverse
proxy. `--public-url` requires `--allow-unauthenticated-network` and changes
the printed URL, not the listener. Crit has no network authentication;
confirm the user wants that exposure before making it reachable remotely.
