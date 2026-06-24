# Crit — Codex CLI Integration

Drop-in configuration files that teach the OpenAI Codex CLI to use Crit for reviewing plans and code changes.

## What's included

| Path | Install command | Purpose |
|------|----------------|---------|
| `skills/crit/SKILL.md` | `crit install codex` | `$crit` skill — launches the interactive review loop |
| `skills/crit-cli/SKILL.md` | `crit install codex` | CLI reference — `crit comment`, `crit pull/push`, review file format |
| `plugin/crit/.codex-plugin/plugin.json` | `crit install codex-plugin` | Codex plugin manifest (skills + hooks) |
| `plugin/crit/skills/*` | `crit install codex-plugin` | Plugin-packaged copies of the skills |
| `plugin/crit/hooks/hooks.json` | `crit install codex-plugin` | `Stop` hook → `crit plan-hook --mode codex` for proposed-plan review |

## Install

**Skills only** (on-demand `$crit` when the target is already a file on disk):

```bash
crit install codex              # project: .agents/skills/
cd ~ && crit install codex      # global: ~/.agents/skills/
```

**Full plugin** (recommended — adds proposed-plan review in Plan mode):

```bash
crit install codex-plugin              # project: plugins/crit/ + .agents/skills/
cd ~ && crit install codex-plugin      # global: ~/.codex/plugins/crit/ + ~/.agents/skills/
```

The plugin install also:

1. Registers Crit in `.agents/plugins/marketplace.json` (project) or `~/.agents/plugins/marketplace.json` (global)
2. Enables `crit@local` in `~/.codex/config.toml`
3. Sets `features.plugins`, `features.hooks`, and `features.plugin_hooks` to `true` in that config

Safe to re-run. Existing files are skipped unless you pass `--force`.

## Plan mode and in-chat plans

In Codex Plan mode, the agent often keeps the plan in chat (`<proposed_plan>`) instead of writing a file. Bare `$crit` needs a path, so it cannot review those plans on its own.

With `crit install codex-plugin`, the `Stop` hook reads the proposed plan from the Codex transcript, writes it to a temp file, and runs `crit plan-hook --mode codex`. You leave inline comments, the agent revises, and the turn does not complete until you approve.

Disable the hook: `export CRIT_PLAN_REVIEW=off`

## Usage

Once installed:

- Type `$crit` in Codex chat to review current git changes, a file, PR, or commit range
- In Plan mode with the plugin, proposed plans are reviewed automatically when the agent tries to finish the turn
- The `crit-cli` skill teaches the agent about `crit comment`, sharing, and GitHub PR sync without manual invocation
