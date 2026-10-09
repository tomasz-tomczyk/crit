---
name: crit-story
description: "Author a crit story and continue the interactive review loop only when the user explicitly invokes /skill:crit-story or directly asks to generate a crit story. Do not infer this skill from generic review, PR, or diff-review requests."
---

# Author a Crit story

Author the story in-session, then continue the interactive Crit review loop.
Do not run bare `crit story` unless the user asks for the `agent_cmd` path.

## 1. Read the guide and diff

```bash
crit story --guide
crit story --prep /tmp/crit-story-prep.txt
```

The guide prints the user's resolved authoring instructions followed by `---`
and the JSON schema. Follow it rather than assuming this skill contains the
current guide. Read the prep file for the full diff and its
`(file_path, old_start)` hunk identifiers.

## 2. Author and ingest

Use the `write` tool to write `/tmp/crit-story.json`. Cluster hunks by theme,
not by file, following the guide. Emit only `prologue`, `chapters`, and
`support`; Crit fills in metadata and coverage.

```bash
crit story --story-file /tmp/crit-story.json
```

Inspect the JSON coverage report on every attempt. Exit 1 means rejection:
fix duplicate or missing hunk assignments and re-ingest. A drift error means
the diff changed; fetch fresh prep and re-author. Exit 0 with
`auto_repaired: true` means omissions were back-filled into `support`.

## 3. Reconnect and await Finish Review

Ingest opens the browser and exits; it does not wait for the human.
Run bare `crit` through omp's `bash` tool as an asynchronous job:

```json
{"command":"crit","async":true,"timeout":0}
```

Save the returned job ID. `timeout: 0` disables the default five-minute
command deadline; `async: true` alone does not. If asynchronous jobs are
unavailable, use foreground `bash` with `timeout: 0`.
Do not use a named service for this finite review-round command.

Relay the review URL verbatim; if omitted from startup output, read
`proc://<job-id>` once. Use `wait` when no independent work remains.
Completion also auto-delivers. A running snapshot or unrelated message
is not completion: keep waiting until this job exits.
Do not poll process output or the review file, hold an eval cell open,
read feedback early, or ask the user to type a reply to signal Finish Review.

## 4. Address feedback and repeat

Read the command output and follow the finish prompt; check for
`approved: true` or `approved: false`. omp merges stdout and stderr.
Retrieve omitted output through `proc://<job-id>` or its `artifact://` link.
A command failure is not approval; report the error rather than proceeding.
If approved, stop. Otherwise read every unresolved comment and its replies,
including review-level comments. Use `quote` and `anchor` rather than stale
line numbers when edits have moved the content.

Address comments against the actual source files. Do not regenerate or edit
the saved story JSON unless the user explicitly asks.

```bash
crit comment --reply-to <id> --author 'omp' '<what changed>'
crit comment --json --file /tmp/crit-story-replies.json --author 'omp'
```

Write bulk reply JSON with the `write` tool; each entry has `reply_to`
and `body`. Do not use `--resolve` unless explicitly requested.
For mid-round re-entry, use `crit comments --json`.

Run the next-round command printed on stdout with the same asynchronous
`bash` pattern and `timeout: 0`. Tell the user only that you replied in Crit
and the changes are ready for review. Do not repeat your replies or list the
comments in chat. Await command completion, and repeat this step until
approved.
Do not call `crit push` or `crit share` unless the user asks.
