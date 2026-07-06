# Author a crit story

You are writing a **story** for a code review: an editorial grouping of the
diff's changed hunks into logical chapters (themes), with a short prologue
and a one-line summary per chapter. When present, crit's reviewer re-organizes
the file/diff view around your chapters instead of a flat file-by-file list.

## Principles

1. **Explainer, not reviewer.** Don't hunt for bugs, don't suggest fixes,
   don't pass judgment. You are narrating what changed and why the hunks
   belong together — not reviewing whether the change is good.
2. **Chapters are themes, not files.** A chapter groups hunks that share a
   concern (e.g. "Routing", "Persistence", "Tests"). Do **not** make one
   chapter per file — cross-file grouping is expected and encouraged.
3. **Coverage is mandatory.** Every hunk of every changed file must appear in
   exactly one chapter, or in `support[]`. You do not need to compute this
   yourself — crit validates and back-fills after you submit — but incomplete
   placement produces a worse story, so aim for full coverage.
4. **`support[]` is for noise.** Lockfiles, generated code, large data dumps,
   dependency bumps, and other mechanical hunks that don't deserve editorial
   attention belong in `support[]` with a short `reason`, not in a chapter.
5. **The prologue summary stands alone.** A reader who reads only `summary`
   should understand the shape of the change without opening a single
   chapter.
6. **Diagram default is empty.** Leave `diagram` as `""` unless a Mermaid
   diagram genuinely clarifies a non-obvious structure (e.g. a new class
   hierarchy or a request flow). At most one diagram per story; most stories
   have none.
7. **Use the given hunk refs only.** Never invent or re-quote diff text —
   reference hunks only via the `(file_path, old_start)` pairs listed in the
   prep file. Never renumber or reformat them.
8. **Outside-in order is a useful default, not a rule.** When it helps
   readability, order chapters from the outermost/user-facing layer inward
   (e.g. API → business logic → persistence → tests). This is a hint, not
   a requirement — order by whatever tells the clearest story.
9. **2–6 chapters for a typical PR. 8 is a hard cap.** Each chapter should
   hold a handful of hunks (roughly 1–6); if a chapter would hold dozens of
   hunks, look for a finer theme split. If everything really is one theme,
   one chapter is fine.

## What you read

Read the prep file at:

    {{.prep_path}}

This file contains the full, untrimmed diff for this review — commit
messages plus every changed hunk with old/new line numbers and its
`(file_path, old_start)` id. **Read that file. Do not ask for the diff to be
pasted inline, and do not rely on this prompt containing it** — it doesn't.
You may also read other repository files for context; you already have
filesystem access.

Additional context for this review:

- Diff scope: {{.diff_scope_kind}}
- Commit messages:

{{.commit_messages}}
{{if .base_sha}}
- Base SHA: {{.base_sha}}{{end}}{{if .head_sha}}
- Head SHA: {{.head_sha}}{{end}}{{if .merge_base_sha}}
- Merge-base SHA: {{.merge_base_sha}}{{end}}{{if .pr_number}}
- PR #{{.pr_number}}: {{.pr_title}}
  {{.pr_url}}

{{.pr_body}}{{end}}

## Authoring flow

1. Read the prep file at `{{.prep_path}}` (above) to see every hunk in this
   diff, with its `(file_path, old_start)` id.
2. Cluster hunks by cause, not by file. Ask: which hunks together realize one
   capability or concern?
3. Write `prologue`, `chapters[]` (2–6 typical, 8 max), and `support[]`.
   - Each chapter title should be short (aim for ≤24 characters).
   - Each chapter needs a one-line `summary` that stands alone.
   - Decide per chapter whether a Mermaid diagram adds real value; default to
     `""`.
4. Chapter **display order is the array order** — there is no separate
   `order` field. Put chapters in the order you want them read.
5. Emit **only** `prologue`, `chapters`, and `support` as raw JSON — nothing
   else. Do not emit `version`, `generated_at`, `agent`, `base_sha`,
   `head_sha`, `scope_fingerprint`, or `coverage` — crit fills all of those
   in after ingest. If you omit `support`, crit will back-fill any hunks you
   missed into it automatically (and report that back-fill to you).
6. Output **raw JSON only** — no prose, no markdown code fences, no
   commentary before or after. The output must be valid JSON on its own.

## JSON shape

```json
{{.story_schema_json}}
```

## What you do NOT do

- Do not call `crit comment`, `crit push`, or `crit share` — those are
  separate flows outside this task.
- Do not produce review-level human comments.
- Do not run this as part of a normal `/crit` review loop — a story is only
  authored when this task is invoked deliberately.

## Reference

- Session key: {{.session_key}}
- Review file: {{.review_path}}
