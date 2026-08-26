# Story mode

Story mode adds an optional narrative layer to a diff review. Instead of
showing only a flat file list, Crit can show a prologue and ordered chapters
that group changed hunks by theme: routing, persistence, tests, generated
files, and so on.

It is an explainer, not a reviewer. A story should help a human understand the
shape of a change before reading the diff. It should not add review comments,
judge the patch, or suggest fixes.

## When to use it

Use story mode for branch, PR, MR, or range reviews where the diff is large
enough that a thematic overview helps:

```bash
crit story
crit story --pr 123
crit story --mr 123
crit story --range main..HEAD
```

`crit story` is diff-scoped only. It does not run for positional file reviews,
live reviews, preview reviews, or plan reviews.

By default, `crit story` uses your global `agent_cmd` to author the story,
saves it into the existing review JSON, starts or updates the review daemon,
and opens the browser at the story view.

```json
{
  "agent_cmd": "claude -p"
}
```

`agent_cmd` must be an agentic CLI that can read files from disk. Story
generation is prompt-by-reference: Crit writes the full prep file to disk and
tells the agent to read it. The diff is not pasted into the prompt.

## Common commands

| Command | Use |
| ------- | --- |
| `crit story` | Generate a story for the current branch diff |
| `crit story --refresh` | Regenerate an existing story |
| `crit story --no-spend` | Reopen an existing story without calling `agent_cmd` |
| `crit story --clear` | Remove the story and return to the flat diff view |
| `crit story --no-open` | Save or resume without opening a browser tab |
| `crit story --skip-llm` | Save a support-only stub story for testing the renderer |
| `crit story --guide` | Print the resolved authoring guide and JSON shape |
| `crit story --prep /tmp/crit-story-prep.txt` | Write the full prep file for manual authoring |
| `crit story --story-file /tmp/crit-story.json` | Ingest pre-authored story JSON |

If a story already exists, `crit story` reopens it. Use `--refresh` to replace
it or `--clear` to remove it.

## What Crit saves

The story is stored on the existing review file under `story`:

```text
~/.crit/reviews/<session-key>/review.json
```

There is no separate story cache or sidecar file in v1. Because story data
lives in the review JSON, it follows the same per-branch or per-range session
key as the rest of the review.

Agents should emit only the editable story fields:

```json
{
  "prologue": {
    "title": "What changed",
    "overview": "A short overview of the shape of the diff.",
    "motivation": "Optional context for why these changes exist.",
    "key_changes": ["Optional concise bullets"],
    "risks": ["Optional concise bullets"],
    "diagram": ""
  },
  "chapters": [
    {
      "id": "ch1",
      "title": "Request routing",
      "summary": "Routes now carry the new story endpoint and state.",
      "hunk_refs": [
        {"file_path": "internal/server/prompts.go", "old_start": 42}
      ],
      "diagram": ""
    }
  ],
  "support": [
    {
      "hunk_refs": [
        {"file_path": "go.sum", "old_start": 120}
      ],
      "reason": "Dependency checksum churn."
    }
  ]
}
```

Crit fills `version`, `generated_at`, `base_sha`, `head_sha`,
`scope_fingerprint`, and `coverage` during ingest. Do not include them in
agent-authored JSON.

## Author a story manually

Manual authoring uses the same validation path as LLM generation:

```bash
crit story --guide > /tmp/crit-story-guide.md
crit story --prep /tmp/crit-story-prep.txt
```

Read both files, then write a JSON object with `prologue`, `chapters`, and
`support` to a temp file:

```bash
crit story --story-file /tmp/crit-story.json
```

On every ingest attempt, Crit prints a coverage report to stdout:

```json
{"ok":true,"indexed":12,"placed":12}
```

Exit code `0` means the story was saved. Exit code `1` means nothing was
saved, usually because the JSON did not parse, a hunk was duplicated, fewer
than half of the hunks were placed, or the diff changed since the prep file was
written.

If `auto_repaired` is true, Crit saved the story but back-filled omitted hunks
into `support[]`:

```json
{"ok":false,"indexed":12,"placed":10,"missing":["(web/app.js, 380)"],"auto_repaired":true}
```

That is acceptable, but usually worth re-authoring if the missing hunk belongs
in a real chapter.

## Write good chapters

A useful story follows a few constraints:

- Group by theme, not by file. Cross-file chapters are expected.
- Keep chapter titles short. Crit truncates titles beyond 48 characters.
- Use chapter array order as the reading order. There is no separate `order`
  field.
- Put generated files, lockfiles, vendored data, and other mechanical changes
  in `support[]`.
- Reference hunks only by the `(file_path, old_start)` pairs from the prep
  file.
- Leave `diagram` empty unless a Mermaid diagram clarifies a real structure or
  flow.
- Prefer 2-6 chapters for a typical PR. One chapter is fine when the change is
  genuinely one theme.

## Make your own story prompt

Story generation uses the `on_story_generate` prompt hook. The stock prompt is
shipped at:

```text
integrations/prompts/on_story_generate.md
```

Install a copy to edit:

```bash
crit install story-prompts
```

Run that from a repo root to create a project prompt:

```text
.crit/prompts/on_story_generate.md
```

Run it from your home directory to create a global prompt:

```text
~/.crit/prompts/on_story_generate.md
```

You can also point config at a custom file:

```json
{
  "prompts": {
    "on_story_generate": "file:.crit/prompts/my-story-guide.md"
  }
}
```

`on_story_generate` has no `:mode` suffix because story mode only applies to
diff scopes. Resolution order is:

1. Project `.crit.config.json` `prompts.on_story_generate`
2. Global `~/.crit.config.json` `prompts.on_story_generate`
3. Project `.crit/prompts/on_story_generate.md`
4. Global `~/.crit/prompts/on_story_generate.md`
5. Stock Crit template

Overrides replace the entire prompt. If you customize
`on_story_generate`, keep the operational requirements in your prompt:

- Tell the agent to read `{{.prep_path}}`.
- Tell it to emit raw JSON only.
- Tell it to emit only `prologue`, `chapters`, and `support`.
- Include `{{.story_schema_json}}` or an equivalent schema.
- Preserve the hunk coverage rule: every hunk appears exactly once in
  `chapters[]` or `support[]`.
- Tell the agent not to call `crit comment`, `crit push`, or `crit share`.

Useful template variables:

| Variable | Meaning |
| -------- | ------- |
| `{{.prep_path}}` | Path to the full prep file the agent must read |
| `{{.story_schema_json}}` | JSON shape Crit can ingest |
| `{{.commit_messages}}` | Commit messages over the diff scope |
| `{{.diff_scope_kind}}` | `committed` or `workingTree` |
| `{{.base_sha}}` / `{{.head_sha}}` / `{{.merge_base_sha}}` | Scope SHAs when available |
| `{{.pr_number}}` / `{{.pr_url}}` / `{{.pr_title}}` / `{{.pr_body}}` | PR context when available |
| `{{.mr_number}}` / `{{.mr_url}}` | MR context when available |
| `{{.session_key}}` | Review session key |
| `{{.review_path}}` | Path to the review JSON file |

Project-level story prompts use the same project prompt trust flow as other
agent prompts. If a project prompt is untrusted, `crit story` will refuse to
use it until you trust project prompts from the Crit UI or choose to use Crit
defaults.

See [Agent prompts](agent-prompts.md) for the full prompt hook reference.
