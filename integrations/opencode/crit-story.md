---
description: Author a crit story (chaptered diff overview) — only when explicitly invoked
agent: build
---

# Author a crit story with `crit story`

Invoke this only when the user runs `/crit-story` or directly asks you to
generate a crit story. Do not infer it from generic review, PR, `/crit`, or
diff-review requests.

Primary path: author in-session with `--guide` / `--prep` / `--story-file`
(do **not** run bare `crit story`, which spends `agent_cmd` tokens).

## Step 1: Fetch the guide

```bash
crit story --guide
```

Read and follow that guide's principles and JSON shape exactly.

## Step 2: Write the prep file

```bash
crit story --prep /tmp/crit-story-prep.txt
```

**Read that file** — the diff is never inlined into the guide prompt.

## Step 3: Author the story JSON

Cluster hunks by theme (not by file). Write JSON with **only** `prologue`,
`chapters`, and `support` to `/tmp/crit-story.json`. Crit fills metadata.

## Step 4: Ingest

```bash
crit story --story-file /tmp/crit-story.json
```

Exit 0 = saved (browser opens). Exit 1 = rejected — fix coverage and retry.
On drift, re-run `--prep` and re-author.

## What this does NOT do

- Does not call `crit comment`, `crit push`, or `crit share`
- Does not produce review comments
- Does not run inside the `/crit` review loop
- Does not run bare `crit story` unless the user explicitly asks for that path
