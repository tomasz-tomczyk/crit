---
name: crit-story
description: "Author a crit story only when the user explicitly invokes /crit-story or directly asks you to generate a crit story. Do not infer this skill from generic review, PR, or diff-review requests."
allowed-tools: Bash(crit story:*), Read
argument-hint: ""
---

# Author a crit story with `crit story`

This is a sibling skill to `/crit` — it does not run inside the normal review
loop. Invoke it deliberately only when the user explicitly asks for story
generation; do not infer it from generic review, PR, or diff-review requests.

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

Exit 0 means the story was saved (crit opens the browser to show it). Exit 1
means it was rejected — the coverage report (missing/duplicated hunks) is
printed to stdout as JSON on every attempt, success or failure. If rejected:

- `duplicated` non-empty: a hunk is claimed by two chapters (or a chapter and
  support). Decide where it belongs and re-ingest.
- `missing` non-empty with `auto_repaired: true` on exit 0: crit already
  back-filled the omissions into `support[]`. Optionally re-author to place
  them deliberately.
- A drift error ("diff changed since prep"): re-run `crit story --prep` and
  re-author from the fresh prep file.

## What this skill does NOT do

- It does not call `crit comment`, `crit push`, or `crit share` — those are
  separate flows.
- It does not produce review-level human comments.
- It does not run as part of the generic `/crit` review loop.
