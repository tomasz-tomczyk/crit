# Command hooks

Crit can run **your** shell scripts when a review finishes or is approved — deterministic, auditable side effects, no LLM in the loop. This is the executable counterpart to [agent prompts](agent-prompts.md): prompts feed the agent *text*; command hooks *do* things.

## At a glance

- **What:** user-defined shell commands/scripts executed at the same finish lifecycle points as prompt templates (`on_finish_unresolved` / `on_finish_approved`, optionally mode-suffixed `:files` / `:diff` / `:live` / `:preview`).
- **Why:** deterministic side effects the agent prompt can't reliably do itself — snapshot commented-on files to a dataset, write an audit log, notify Slack, etc.
- **How:** `inline:<cmd>` (run via `sh -c`) or `file:<path>` (exec'd directly, shebang respected). Crit pipes a JSON payload to the hook's stdin and sets `CRIT_*` env vars (review path, session key, mode, counts, files-with-comments, …).
- **Opt-in:** Crit runs zero command hooks by default. You configure them in the `hooks` map of `~/.crit.config.json` / `.crit.config.json` or drop scripts at `.crit/hooks/*.sh`.
- **Trust:** project-level hooks run arbitrary code and go through the same trust gate as project prompts — Finish is blocked until you trust the project's hook config/files. Global hooks (user-installed) run without the gate.
- **Timeout:** each hook is capped at 60 seconds (killed and warned on timeout). Network-bound or long-running work should fork-and-detach from the hook.
- **Failure never blocks finish:** a hook that errors, times out, or exits non-zero is logged as a warning; the review flow proceeds regardless.

Command hooks are **opt-in and not used by default** — Crit runs zero command hooks out of the box. Config lives under the `hooks` map (`~/.crit.config.json` and/or `.crit.config.json`) and/or conventional files under `.crit/hooks/` (project) and `~/.crit/hooks/` (global). They reuse the same hook names, mode suffixes, resolution order, and project trust flow as prompt templates.

## When hooks fire

| Hook | Fires when |
| --- | --- |
| `on_finish_unresolved` | Finish review with open comments (fallback for all modes) |
| `on_finish_unresolved:files` | Unresolved finish — single-file or plan review |
| `on_finish_unresolved:diff` | Unresolved finish — branch / PR / range review |
| `on_finish_unresolved:live` | Unresolved finish — live URL review |
| `on_finish_unresolved:preview` | Unresolved finish — static HTML preview |
| `on_finish_approved` | Approve with zero unresolved comments (fallback) |
| `on_finish_approved:files` / `:diff` / `:live` / `:preview` | Mode-specific approve hooks |

Resolution order for e.g. `on_finish_unresolved` in a PR review:

1. `on_finish_unresolved:diff` (if set)
2. `on_finish_unresolved` (fallback)

Internally, git/Sapling/JJ branch/PR/range reviews use mode `diff`; plan and file-based reviews use `files`. This is identical to the prompt system.

Only **one** hook fires per finish: the most specific key that resolves wins (mode-specific over generic). There is no stock hook — if nothing is configured, nothing executes.

## Configuration

**Global:** `~/.crit.config.json` `hooks` map and/or `~/.crit/hooks/`
**Project:** `.crit.config.json` `hooks` map and/or `.crit/hooks/` in the repo root (committable, team-shared)

### How paths are resolved

Precedence for each hook (e.g. `on_finish_unresolved` in a PR review) is identical to prompts:

1. **Project** `.crit.config.json` `hooks` entry (mode-specific key, then generic)
2. **Global** `~/.crit.config.json` `hooks` entry (same fallback order)
3. **Project** conventional file under `.crit/hooks/` (e.g. `on_finish_unresolved.diff.sh`, then `on_finish_unresolved.sh`)
4. **Global** conventional file under `~/.crit/hooks/` (same naming)
5. Nothing — no stock command hook is run.

Config `file:` paths and conventional filenames both use `.` instead of `:` for mode suffixes (`on_finish_unresolved:diff` → `on_finish_unresolved.diff.sh`).

You do **not** need a `hooks` map entry when the script already lives at the conventional path. Copy the example scripts from `docs/example-hooks/` into place:

- **Global:** `cp docs/example-hooks/*.sh ~/.crit/hooks/` (create the dir first)
- **Project:** `cp docs/example-hooks/*.sh .crit/hooks/` (from your repo root)

The example scripts are **reference material** — copying them does not enable behavior until you edit them (or add a `hooks` config map entry) to opt in.

**Discovered** hook files under `.crit/hooks/` must be named `on_finish_*.sh` (mode suffix uses `.` not `:`). For other extensions or paths, use an explicit `file:` entry in the `hooks` config map — Crit execs the script directly and respects its shebang.

Explicit `hooks` config still wins over conventional files and is useful for non-standard paths or `inline:` overrides.

| Config key | Typical project path |
| --- | --- |
| `on_finish_unresolved` | `.crit/hooks/on_finish_unresolved.sh` |
| `on_finish_unresolved:diff` | `.crit/hooks/on_finish_unresolved.diff.sh` |
| `on_finish_approved` | `.crit/hooks/on_finish_approved.sh` |
| `on_finish_approved:files` | `.crit/hooks/on_finish_approved.files.sh` |

Config keys use `:` for mode suffixes; filenames use `.` instead.

`agent_cmd`, `auth_token`, and `share_url` stay global-only; `hooks` is allowed in project config (but project hooks are gated by trust — see below).

### Value forms

| Form | Example | Use |
| --- | --- | --- |
| `inline:…` | `"inline:rsync \"$CRIT_REVIEW_PATH\" ~/reviews/"` | One-liner commands run via `sh -c` |
| `file:…` | `"file:.crit/hooks/on_finish_approved.sh"` | A script exec'd directly (shebang + exec bit respected) |

`inline:` values run as a single `sh -c` command on Unix (`cmd /c` on Windows). `file:` values exec the resolved path directly so its `#!/interpreter` line applies; the file must be executable (`chmod +x`) on Unix.

### Example project config

```json
{
  "hooks": {
    "on_finish_approved": "file:.crit/hooks/on_finish_approved.sh",
    "on_finish_unresolved": "file:.crit/hooks/on_finish_unresolved.sh",
    "on_finish_unresolved:diff": "inline:echo \"unresolved diff finish\" >> ~/.crit/reviews/finishes.log"
  }
}
```

## What the hook receives

Hooks run **synchronously** during `POST /api/finish`, **after** the review file has been persisted to disk (so `$CRIT_REVIEW_PATH` is readable). The working directory is the repo root. A hook that times out or exits non-zero is logged as a warning and **never blocks finish** — the agent still gets its prompt.

### Environment variables (snake_case, `CRIT_`-prefixed)

All `CRIT_*` env vars are strings (shell env vars can only carry strings). Numeric and boolean values are conveyed as decimal/`"true"`/`"false"` string literals; the stdin JSON carries them as native types.

| Variable | Description |
| --- | --- |
| `CRIT_REVIEW_PATH` | Path to the review JSON file (`~/.crit/reviews/<key>.json`) |
| `CRIT_SESSION_KEY` | Daemon session key |
| `CRIT_MODE` | `files`, `diff`, `live`, or `preview` |
| `CRIT_APPROVED` | `true` or `false` |
| `CRIT_UNRESOLVED_COUNT` | Open comments at finish time |
| `CRIT_TOTAL_COUNT` | Total comments in the session |
| `CRIT_FILES_WITH_COMMENTS` | Newline-joined repo-relative paths with unresolved comments |
| `CRIT_FILES_WITH_COMMENTS_COUNT` | Count of the above |
| `CRIT_PLAN_SLUG` | Plan slug when reviewing a plan file |
| `CRIT_INTERNAL_SESSION_MODE` | `files`, `git`, or `plan` |
| `CRIT_COMMENTS_CMD` | `crit comments --json '<review>'` — retrieve unresolved comments |
| `CRIT_COMMENTS_ALL_CMD` | `crit comments --json --all '<review>'` — all comments |
| `CRIT_NEXT_ROUND_CMD` | Command to start the next round |
| `CRIT_COMMENTS_UNRESOLVED_JSON` | Unresolved comment threads as a JSON array |
| `CRIT_COMMENTS_JSON` | All comments in the session as a JSON array |
| `CRIT_SESSION_DURATION_SECONDS` | Session duration (when stats available) |
| `CRIT_SESSION_FILES_REVIEWED` | Files reviewed |
| `CRIT_SESSION_COMMENTS_SUBMITTED` | Comments you submitted |

### stdin

A pretty-printed JSON object with the same fields as the env vars (snake_case), plus an explicit `files_with_comments` array and a `session_stats` object. Empty comment arrays render as `[]` (never `null`), so `jq`/awk always see an array.

```sh
payload="$(cat)"
echo "$payload" | jq -r '.files_with_comments[]'
```

### stdout / stderr

Hook **stdout and stderr are captured, not forwarded to the agent.** The agent-facing prompt (Crit's blocking `stdout`) is untouched — prompts and command hooks are independent. Crit logs a one-line summary on success (`exit=N stdout=NB stderr=NB`) and prints captured stderr on failure so you can debug. Hook stdout is recorded in the daemon log but not echoed to the terminal by default.

### Timeout

Each hook is capped at **60 seconds**. A hung hook is killed and reported as a warning; finish proceeds. (Network-bound or long-running work should fork-and-detach from the hook.)

## Project hook trust

Project-level **command hooks run arbitrary code**, so they are gated by the *same* trust flow as project prompts. A checked-in `.crit.config.json` `hooks` entry or `.crit/hooks/*.sh` triggers the trust dialog before Finish/Approve:

1. Everything else works normally — browse, comment, reply, Send now.
2. **Finish / Approve is blocked** until you choose:
   - **Trust until prompts change** (recommended) — re-prompt if `.crit.config.json`, any `file:` hook path in the config map, or any discovered `.crit/hooks/*.sh` changes. Crit hashes the **full file contents** of every referenced/discovered hook script (same as prompt templates), so editing a `.sh` body invalidates trust even when the filename stays the same.
   - **Always trust this project** — use project prompts + hooks on future changes without re-prompting
   - **Use Crit defaults** — ignore project prompts *and* project hooks for this repo
3. The trust dialog lists every source file (including `.crit/hooks/*.sh`).

The trust decision is stored in global config under `trusted_project_prompts` (keyed by repo root hash) — the same store already used for prompt trust, extended to cover hooks. **Global** hooks (`~/.crit.config.json` / `~/.crit/hooks/`) are user-installed and run without the trust dialog.

## Examples

### Snapshot commented-on files

Build a dataset of "things the agent got wrong" by copying each commented-on file alongside the review JSON:

```sh
#!/usr/bin/env sh
# .crit/hooks/on_finish_unresolved.sh
set -eu
: "${CRIT_REVIEW_PATH:?}" "${CRIT_SESSION_KEY:?}"

[ "${CRIT_UNRESOLVED_COUNT:-0}" -eq 0 ] && exit 0

dest="$HOME/.crit/reviews/references/$CRIT_SESSION_KEY"
mkdir -p "$dest"
cp "$CRIT_REVIEW_PATH" "$dest/review.json"

printf '%s\n' "$CRIT_FILES_WITH_COMMENTS" | while IFS= read -r f; do
  [ -n "$f" ] && [ -f "$f" ] || continue
  mkdir -p "$dest/files/$(dirname "$f")"
  cp "$f" "$dest/files/$f"
done

echo "crit: snapshotted $CRIT_UNRESOLVED_COUNT comment(s) to $dest" >&2
```

This mirrors the example shipped at `docs/example-hooks/on_finish_unresolved.sh` — copy it into `.crit/hooks/` or `~/.crit/hooks/` and edit to taste.

($PWD is the repo root, so `$CRIT_FILES_WITH_COMMENTS` entries are repo-relative and copy directly. When there are no per-file comments — e.g. file-level / review-level comments only — the script falls back to deriving paths from the stdin JSON via `jq` if available.)

### Audit log on approve

```sh
#!/usr/bin/env sh
# ~/.crit/hooks/on_finish_approved.sh
: "${CRIT_REVIEW_PATH:?}"
log="$HOME/.crit/reviews/approved.log"
mkdir -p "$(dirname "$log")"
printf '%s\t%s\t%s\t%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${CRIT_SESSION_KEY:-?}" "$CRIT_MODE" "$CRIT_REVIEW_PATH" >>"$log"
```

### Drive an external tool deterministically

```json
{
  "hooks": {
    "on_finish_unresolved:diff": "inline:~/.crit/hooks/post-to-slack \"crit review finished\" \"unresolved=$CRIT_UNRESOLVED_COUNT mode=$CRIT_MODE\""
  }
}
```

## Limitations & security

- **Project hooks execute arbitrary code.** Treat a checked-in `.crit/hooks/*.sh` or project `hooks` config the way you'd treat a post-install npm script — the trust flow is your gate, but once trusted, the script runs on every Finish/Approve.
- A 60s timeout caps each hook; long work must detach.
- Hook output never reaches the agent prompt. If you want to *instruct* the agent (e.g. change what it does next), use [agent prompts](agent-prompts.md), not command hooks.

## See also

- [Agent prompts](agent-prompts.md) — the template-based counterpart that feeds the agent text
- [Configuration](../README.md#configuration) — `crit config --generate`, global vs project keys