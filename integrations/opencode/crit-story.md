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

## Step 5: Reconnect and wait for Finish Review

**CRITICAL.** Ingest opens the UI and exits. Run bare `crit` and wait until it
exits (same wait mechanics as the `/crit` / `crit` skill for this agent). Do
not proceed until Finish Review.

```bash
crit
```

## Steps 6–8: Review cycle

Same loop as `/crit`: read finish stdout → address comments on **source files**
(not the story JSON) with `crit comment --reply-to` → run `crit` again for the
next round → stop when approved.

## Out of scope during authoring

- Do not produce agent-authored review comments while writing the story.
- Do not run bare `crit story` (LLM via `agent_cmd`) unless the user asks.
- After ingest, enter the normal review cycle (Steps 5–8); do not edit the
  saved story JSON unless the user explicitly asks.
