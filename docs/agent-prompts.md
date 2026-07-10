# Agent prompts

Crit can inject **your** instructions when a review finishes or is approved — without Crit choosing workflows on your behalf.

Prompt hooks are **templates** (Go `text/template`), not shell commands. They feed the finish modal, blocking `crit` stdout (plain text), and plan hooks.

## When hooks fire

| Hook | Fires when |
| ---- | ---------- |
| `on_finish_unresolved` | Finish review with open comments (fallback for all modes) |
| `on_finish_unresolved:files` | Unresolved finish — single-file or plan review |
| `on_finish_unresolved:diff` | Unresolved finish — branch / PR / range review |
| `on_finish_unresolved:story` | Unresolved finish — branch / PR / range review with story mode present |
| `on_finish_unresolved:live` | Unresolved finish — live URL review |
| `on_finish_unresolved:preview` | Unresolved finish — static HTML preview |
| `on_finish_approved` | Approve with zero unresolved comments (fallback) |
| `on_finish_approved:files` / `:diff` / `:story` / `:live` / `:preview` | Mode-specific approve hooks |
| `on_story_generate` | Before `crit story` builds the prompt sent to `agent_cmd` to author a story, and when `crit story --guide` resolves the guide to print |

Resolution order for e.g. `on_finish_unresolved` in a PR review:

1. `on_finish_unresolved:diff` (if set)
2. `on_finish_unresolved` (fallback)

Internally, git, Sapling, and JJ branch/PR/range reviews use mode `diff`; plan and file-based reviews use `files`.

When a diff review has a saved story, finish prompts use mode `story`, not
`diff`. Story finish resolution is intentionally different:

1. `on_finish_unresolved:story` (if set)
2. Stock Crit `on_finish_unresolved.story.md`
3. `on_finish_unresolved` (fallback)
4. Stock Crit `on_finish_unresolved.md`

The same order applies to `on_finish_approved:story`. Crit does not fall back
from story mode to `:diff`, because story mode is a generated editorial view
over the diff and needs different agent instructions.

`on_story_generate` has **no `:mode` split** — `crit story` only operates on diff scopes (git / `--pr` / `--range`), so there's nothing to disambiguate. See [Story generation prompt](#story-generation-prompt-on_story_generate) below.

## Configuration

**Global:** `~/.crit.config.json` and/or `~/.crit/prompts/`  
**Project:** `.crit.config.json` and/or `.crit/prompts/` in the repo root (committable, team-shared)

### How paths are resolved

**Precedence** for each hook (e.g. `on_finish_unresolved` in a PR review):

1. **Project** `.crit.config.json` `prompts` entry (mode-specific key, then generic)
2. **Global** `~/.crit.config.json` `prompts` entry (same fallback order)
3. **Project** conventional file under `.crit/prompts/` (e.g. `on_finish_unresolved.diff.md`, then `on_finish_unresolved.md`)
4. **Global** conventional file under `~/.crit/prompts/` (same naming)
5. **Stock Crit** built-in defaults (when nothing above matches)

Story mode is the exception: project/global `:story` overrides are checked
first, then Crit's stock story template is used before generic user prompts.

Config `file:` paths and conventional filenames both use `.` instead of `:` for mode suffixes (`on_finish_unresolved:diff` → `on_finish_unresolved.diff.md`).

You do **not** need a `prompts` map entry when the file already lives at the conventional path. Install stock templates the same way as agent integrations:

- **Global:** `cd ~ && crit install prompts` → copies to `~/.crit/prompts/`
- **Project:** from your repo root, `crit install prompts` → copies to `.crit/prompts/`

Or copy manually from [`integrations/prompts/`](../integrations/prompts/). Crit picks them up automatically (project beats global).

Explicit `prompts` config still wins over conventional files and is useful for non-standard paths or `inline:` overrides.

| Config key | Typical project path |
| ---------- | -------------------- |
| `on_finish_unresolved` | `.crit/prompts/on_finish_unresolved.md` |
| `on_finish_unresolved:diff` | `.crit/prompts/on_finish_unresolved.diff.md` |
| `on_finish_unresolved:story` | `.crit/prompts/on_finish_unresolved.story.md` |
| `on_finish_approved` | `.crit/prompts/on_finish_approved.md` |
| `on_finish_approved:files` | `.crit/prompts/on_finish_approved.files.md` |

Config keys use `:` for mode suffixes; filenames use `.` instead (e.g. `on_finish_unresolved:diff` → `on_finish_unresolved.diff.md`).

`agent_cmd`, `auth_token`, and `share_url` stay global-only; `prompts` is allowed in project config.

### Shipped templates

| Path | Purpose |
| ---- | ------- |
| [`integrations/prompts/on_finish_approved.md`](../integrations/prompts/on_finish_approved.md) | Stock approve message |
| [`integrations/prompts/on_finish_approved.story.md`](../integrations/prompts/on_finish_approved.story.md) | Stock story-mode approve message |
| [`integrations/prompts/on_finish_unresolved.md`](../integrations/prompts/on_finish_unresolved.md) | Stock unresolved finish (count, embedded comments, actions, reconnect) |
| [`integrations/prompts/on_finish_unresolved.story.md`](../integrations/prompts/on_finish_unresolved.story.md) | Stock story-mode unresolved finish with instructions to edit source files, not saved story JSON |
| [`integrations/prompts/on_story_generate.md`](../integrations/prompts/on_story_generate.md) | Stock story authoring guide — the **entire** prompt sent to `agent_cmd` for `crit story`, principles + JSON shape included |
| [`integrations/prompts/examples/`](../integrations/prompts/examples/) | Optional playbooks (large-PR batching, AGENTS.md extraction, etc.) |

Copy the defaults with `cd ~ && crit install prompts` (global) or `crit install prompts` from your repo root, wire them in `.crit.config.json` if you use non-standard paths, and customize from there.

### Install command

| Command | Copies |
| ------- | ------ |
| `crit install prompts` | `on_finish_approved.md` + `on_finish_approved.story.md` + `on_finish_unresolved.md` + `on_finish_unresolved.story.md` |
| `crit install story-prompts` | `on_story_generate.md` |

Both follow the same global-vs-project rule: run from `$HOME` to install to
`~/.crit/prompts/`, or from a repo root to install to `.crit/prompts/`. Pass
`--force` to overwrite an existing file.

### Value forms

| Form | Example | Use |
| ---- | ------- | --- |
| `inline:…` | `"inline:Reply only, no code changes."` | Single-line overrides |
| `file:…` | `"file:.crit/prompts/on_finish_approved.md"` | Multiline markdown playbooks (preferred) |

`inline:` values must be one line in JSON. Use `file:` for multiline templates.

### Example project config

```json
{
  "prompts": {
    "on_finish_approved": "file:.crit/prompts/on_finish_approved.md",
    "on_finish_unresolved": "file:.crit/prompts/on_finish_unresolved.md",
    "on_finish_unresolved:diff": "file:.crit/prompts/on_finish_unresolved.diff.md"
  }
}
```

## Template variables

Templates receive these variables (snake_case in templates):

| Variable | Description |
| -------- | ----------- |
| `{{.review_path}}` | Path to the review JSON file |
| `{{.comments_cmd}}` | Command to retrieve unresolved comments only — `crit comments --json '…'` |
| `{{.comments_all_cmd}}` | All comments — `crit comments --json --all '…'` |
| `{{.next_round_cmd}}` | Command to start the next round (`crit`, `crit --session …`, `crit plan …`) |
| `{{.session_key}}` | Daemon session key |
| `{{.mode}}` | `files`, `diff`, `live`, or `preview` |
| `{{.unresolved_count}}` | Open comments at finish time |
| `{{.total_count}}` | Total comments in the session |
| `{{.files_with_comments}}` | List of file paths with unresolved comments |
| `{{.plan_slug}}` | Plan slug when reviewing a plan file |
| `{{.comments_unresolved_json}}` | JSON array of unresolved comments (threads where `resolved` is false) — stock unresolved finish embeds this in stdout |
| `{{.comments_json}}` | JSON array of **all** comments in the session (resolved and unresolved) |
| `{{.session_stats.duration_seconds}}` | Session duration (when available) |
| `{{.session_stats.files_reviewed}}` | Files reviewed |
| `{{.session_stats.comments_submitted}}` | Comments you submitted |

**Conditionals:** [Go `text/template` syntax](https://pkg.go.dev/text/template), e.g. `{{if gt .unresolved_count 10}}…{{else}}…{{end}}`.

## Story generation prompt (`on_story_generate`)

`crit story` groups a diff's changed hunks into editorial chapters (see
`crit story --help`). Before it execs `agent_cmd` to author that story — and
whenever `crit story --guide` resolves the guide to print — it renders the
`on_story_generate` hook through the **same 5-level precedence** as the
finish hooks above, minus the `:mode` split (story is diff-scoped only):

1. Project `.crit.config.json` `prompts.on_story_generate`
2. Global `~/.crit.config.json` `prompts.on_story_generate`
3. Project `.crit/prompts/on_story_generate.md`
4. Global `~/.crit/prompts/on_story_generate.md`
5. Stock template ([`integrations/prompts/on_story_generate.md`](../integrations/prompts/on_story_generate.md))

Project-level overrides go through the same [project prompt trust](#project-prompt-trust) mechanism as `on_finish_*` — an untrusted project's `on_story_generate` override is ignored (falls through to global/stock) until you trust it.

**Override semantics are replace, not append.** Whichever level resolves
becomes the entire prompt sent to `agent_cmd` — there's no `{{.story_guide_md}}`
indirection to a separate file, and no "keep crit's constraints" hybrid.
If you override `on_story_generate`, you own the whole authoring guide,
including the JSON-shape instructions the agent needs to produce a story
crit can ingest.

Install the stock template with `crit install story-prompts` (see
[Install command](#install-command) below), then edit
`.crit/prompts/on_story_generate.md` or `~/.crit/prompts/on_story_generate.md`
directly.

### Story template variables

In addition to the shared variables above (`{{.session_key}}`,
`{{.review_path}}`), `on_story_generate` receives:

| Variable | Description |
| -------- | ----------- |
| `{{.prep_path}}` | Path to the prep file on disk (full, untrimmed hunks with `(file_path, old_start)` ids). The prompt instructs the agent to **read this file** — the diff is never inlined into the prompt. |
| `{{.story_schema_json}}` | JSON shape the agent must emit (`prologue`, `chapters`, `support` only — `crit story` fills in `version`, `generated_at`, `base_sha`, `head_sha`, `scope_fingerprint`, `coverage` after ingest) |
| `{{.commit_messages}}` | `git log --oneline`-style commit messages over the diff scope |
| `{{.diff_scope_kind}}` | `"committed"` or `"workingTree"` |
| `{{.base_sha}}` / `{{.head_sha}}` / `{{.merge_base_sha}}` | SHAs for the diff scope (PR / range reviews) |
| `{{.pr_number}}` / `{{.pr_url}}` / `{{.pr_title}}` / `{{.pr_body}}` | PR-mode variables |

### `agent_cmd` must be an agentic CLI

`crit story` reuses the existing `agent_cmd` config key (global-only, see
[Send to agent](../README.md#send-to-agent-experimental)) — there is no
separate story-specific LLM config. The prompt is **by reference**: it never
inlines the diff, it tells the agent to read `{{.prep_path}}`. This means
`agent_cmd` must be an agentic CLI with filesystem access that can read a
file from the prompt and write JSON to stdout — e.g. `claude -p` or
`codex exec`. A bare completion-API pipe (curl to a chat endpoint with no
tool access) was never supported for `agent_cmd`, including for the existing
"Send to agent" comment-reply feature this reuses.

### `crit story --guide` output

`crit story --guide` prints the resolved `on_story_generate` template body,
then `\n\n---\n\n`, then the JSON schema in a ```` ```json ```` fenced block,
and exits 0. This lets a skill-less agent (or a human) pipe it directly:
`crit story --guide | less`, or extract the schema with a fence-aware tool.
It always re-resolves at runtime, so a customized template take effect
immediately — the `/crit-story` skill deliberately calls `crit story --guide`
rather than embedding a copy of the stock guide in its own skill body.

## Project prompt trust

Project-level prompts are treated like untrusted `AGENTS.md` until you confirm them.

1. Everything else works normally — browse, comment, reply, Send now.
2. **Finish / Approve is blocked** until you choose:
   - **Trust until prompts change** (recommended) — re-prompt if `.crit.config.json` or referenced files change
   - **Always trust this project** — use project prompts on future changes without re-prompting
   - **Use Crit defaults** — ignore project prompts for this repo
3. The trust dialog shows **rendered previews** for each configured hook and lists source files.
4. The finish modal always shows the final `prompt` before copy/send.

Trust is stored in global config under `trusted_project_prompts` (keyed by repo root hash).

## Defaults and finish JSON fields

When no project or global template matches, behavior is unchanged from stock Crit defaults (except blocking `crit` stdout is **plain text**, not JSON — see below).

### Blocking `crit` stdout (agents)

| Output | Content |
| ------ | ------- |
| **stdout** | Rendered `prompt` text — stock defaults embed `comments_unresolved_json` on unresolved finish and `comments_json` on approve. Custom templates choose what to include. |
| **stderr** | `approved: true` or `approved: false`, plus session stats on approve |

`/api/finish` and `/api/review-cycle` still return JSON for the browser and plan hooks. Only the foreground `crit` client writes text to stdout.

### Finish API JSON (browser / hooks)

| Field | Consumer | Stock behavior |
| ----- | -------- | -------------- |
| `comments` | Structured consumers | Unresolved comment objects (empty on approve) |
| `prompt` | Blocking stdout, finish modal, clipboard, plan hooks | Full rendered body (count line, embedded comments JSON, actions, reconnect) |

Custom templates choose what to include. Omit `{{.comments_unresolved_json}}` / `{{.comments_json}}` to keep comment data out of the prompt text; the API `comments` field is unchanged.

## Limitations

- Prompt text is not executed as shell, but a malicious project template could still social-engineer an agent. Trust project prompts deliberately.
- Crit cannot reliably switch harness permission modes (e.g. Claude auto-edit). Use `on_finish_approved` to *instruct* the agent if you want a mode hint.

## See also

- [Configuration](../README.md#configuration) — `crit config --generate`, global vs project keys
- [Story mode](story-mode.md) — feature overview, manual story authoring, and custom story prompts
- [crit skill](../integrations/) — how agents consume finish JSON
- [crit-story skill](../integrations/claude-code/skills/crit-story/SKILL.md) — the thin shim that invokes `crit story --guide` / `--prep` / `--story-file`
