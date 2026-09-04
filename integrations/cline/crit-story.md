# Crit Story — chaptered diff overview

Use this workflow only when the user explicitly invokes `/crit-story` (or this
workflow) or directly asks you to generate a crit story. Do not infer it from
`/crit.md`, a PR review, or a generic "review this" request.

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

## Out of scope

Do not call `crit comment`, `crit push`, or `crit share`. Do not leave review
comments. Do not fold this into the `/crit.md` review loop.
