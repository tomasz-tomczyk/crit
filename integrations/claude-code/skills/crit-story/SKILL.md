---
name: crit-story
description: "Author a crit story and continue the interactive review loop only when the user explicitly invokes /crit-story or directly asks you to generate a crit story. Do not infer this skill from generic review, PR, or diff-review requests."
allowed-tools: Bash(crit:*), Bash(crit story:*), Bash(crit comment:*), Read, Edit
argument-hint: ""
---

# Author a crit story with `crit story`

This skill authors a story-mode overview, then enters the same interactive
review cycle as `/crit` (wait → address comments → next round). Invoke it
only when the user explicitly asks for story generation; do not infer it from
generic review, PR, or diff-review requests.

Primary path: author in-session with `--guide` / `--prep` / `--story-file`
(do **not** run bare `crit story`, which spends `agent_cmd` tokens), then run
bare `crit` to listen for Finish Review.

## Step 1: Fetch the guide

```bash
crit story --guide
```

This prints the resolved authoring guide (the user's customized version if
they have one — always invoke this at runtime rather than reusing this
skill's own prose) followed by `---` and the JSON schema for the fields you
must emit. Read and follow that guide's principles and JSON shape exactly —
it is the source of truth, not this file.

## Step 2: Write the prep file

```bash
crit story --prep /tmp/crit-story-prep.txt
```

This writes the full, untrimmed diff (commit messages + every hunk with its
`(file_path, old_start)` id) to the given path and prints the path. **Read
that file** — the diff is never inlined into the guide prompt.

## Step 3: Author the story JSON

Following the guide from Step 1, cluster hunks by theme (not by file) and
write a JSON object with **only** `prologue`, `chapters`, and `support` to a
temp file, e.g. `/tmp/crit-story.json`. Do not include `version`,
`generated_at`, `agent`, `base_sha`, `head_sha`, `scope_fingerprint`, or
`coverage` — crit fills those in.

## Step 4: Ingest

```bash
crit story --story-file /tmp/crit-story.json
```

Exit 0 means the story was saved (crit opens the browser to show it). **Then continue to Step 5** — do not stop after ingest. Exit 1
means it was rejected — the coverage report (missing/duplicated hunks) is
printed to stdout as JSON on every attempt, success or failure. If rejected:

- `duplicated` non-empty: a hunk is claimed by two chapters (or a chapter and
  support). Decide where it belongs and re-ingest.
- `missing` non-empty with `auto_repaired: true` on exit 0: crit already
  back-filled the omissions into `support[]`. Optionally re-author to place
  them deliberately.
- A drift error ("diff changed since prep"): re-run `crit story --prep` and
  re-author from the fresh prep file.

## Step 5: Reconnect and wait until Finish Review

**CRITICAL — you MUST run this step after a successful ingest. Do NOT skip it. Do NOT proceed without it.**

Story ingest opens the review UI and exits — it does **not** wait for the human. Run `crit` **in the background** using `run_in_background: true`:

```bash
crit
```

`crit` reconnects to the story session's daemon (already started by ingest) and blocks until the user clicks "Finish Review".

If ingest printed a review URL, relay it verbatim:

> **"Crit is open at http://localhost:<port>. Leave inline comments on the diff, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do NOT ask the user to type anything. Do NOT read the review file early. Wait for the background task to finish — that is how you know the human is done reviewing.

## Step 6: Read the review output

When `crit` completes, read **stdout** and follow its instructions. Check **stderr** for `approved: true` or `approved: false`.

If the finish prompt says the review is approved / `"approved": true`, tell the user no changes were requested and stop.

<important if="a comment has a quote, anchor, or drifted field">
- `quote`: the specific text the reviewer selected — focus your changes on the quoted text rather than the entire line range
- `anchor`: use it to locate the current position of the content; line numbers may be stale after edits
- `drifted: true`: original content was removed or heavily rewritten — line numbers are approximate at best
</important>

**Fallback** (mid-round re-entry or headless workflows): `crit comments` / `crit comments --json`.

## Step 7: Address each review comment

Story mode is an editorial view over the underlying diff. Address comments against the **actual source files**. Do **not** edit, regenerate, or patch the saved story JSON unless the user explicitly asks.

For each unresolved comment:

1. Understand what the comment asks for
2. If it contains a suggestion block, apply that specific change
3. Revise the referenced source file using Edit
4. Reply with what you did: `crit comment --reply-to <id> --author 'Claude Code' '<what you did>'` (reply bodies support markdown)
5. **Do not pass `--resolve`.** Resolving is the reviewer's call. Only add `--resolve` if the user explicitly asks.

<important if="you are replying to multiple comments at once">
Use `--json` for a single bulk call instead of one invocation per comment:

```bash
echo '[
  {"reply_to": "c_a1b2c3", "body": "Fixed"},
  {"reply_to": "c_d4e5f6", "body": "Refactored as suggested"}
]' | crit comment --json --author 'Claude Code'
```
</important>

## Step 8: Signal completion and start next round

**CRITICAL — you MUST run this step. Do NOT skip it. Do NOT proceed without it.**

The finish prompt on stdout includes the command to run again — use it to start a new round.

On subsequent calls, `crit` automatically signals round-complete first, then blocks until the next "Finish Review" click.

Tell the user: **"Changes applied. Review the diff in your browser and click Finish Review when ready."**

**Do NOT proceed until `crit` completes.** When it does, return to Step 6. If the user finishes with zero comments, the review is approved — stop the loop and proceed.

## What this skill does NOT do

- It does not produce agent-authored review comments during story authoring —
  humans leave comments in the browser after ingest.
- It does not edit or regenerate the saved story JSON during the review loop
  unless the user explicitly asks.
- It does not run bare `crit story` (LLM via `agent_cmd`) unless the user
  explicitly asks for that path.
- It does not call `crit push` or `crit share` unless the user asks to share.
