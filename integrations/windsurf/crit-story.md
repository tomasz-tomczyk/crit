# Crit Story — chaptered diff overview

Use this workflow only when the user explicitly invokes `/crit-story` or
directly asks you to generate a crit story. Do not infer it from a normal
`/crit` review, PR review, or generic "review this" request.

Primary path: author in-session with `--guide` / `--prep` / `--story-file`
(do **not** run bare `crit story`, which spends `agent_cmd` tokens).

## Step 1: Guide

```bash
crit story --guide
```

Follow the printed guide and JSON schema exactly.

## Step 2: Prep

```bash
crit story --prep /tmp/crit-story-prep.txt
```

Read that file — every hunk id is `(file_path, old_start)`.

## Step 3: Author JSON

Write only `prologue`, `chapters`, and `support` to `/tmp/crit-story.json`.

## Step 4: Ingest

```bash
crit story --story-file /tmp/crit-story.json
```

Fix coverage failures and retry. On drift, re-prep and re-author.

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
