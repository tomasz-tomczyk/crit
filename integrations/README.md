# Crit Integrations

Drop-in configuration files that teach your AI coding tool to use Crit for reviewing plans and code changes.

## Quick install

```bash
crit install <tool>     # Install for a specific tool in the current project
crit install all        # Install for all supported tools
```

Safe to re-run. Existing files are skipped (use `--force` to overwrite).

**Global install**: run `cd ~ && crit install <tool>` to install to your home directory. The integration is then available across all projects without per-project setup. Each tool reads from a different global path; `crit install` routes the files to the right place automatically. Windsurf is the one exception (no per-tool global config dir) and rejects global install with a clear error.

| Tool | Install command | Project destination | Global destination |
|------|----------------|---------------------|--------------------|
| Claude Code | `crit install claude-code` | `.claude/skills/crit/SKILL.md` + `.claude/skills/crit-cli/SKILL.md` | `~/.claude/skills/crit/SKILL.md` + `~/.claude/skills/crit-cli/SKILL.md` |
| Cursor | `crit install cursor` | `.cursor/skills/crit/SKILL.md` + `.cursor/skills/crit-cli/SKILL.md` | (project only — Cursor has no stable user-level config dir) |
| GitHub Copilot | `crit install github-copilot` | `.github/skills/crit/SKILL.md` + `.github/skills/crit-cli/SKILL.md` | `~/.agents/skills/crit/SKILL.md` + `~/.agents/skills/crit-cli/SKILL.md` |
| OpenCode | `crit install opencode` | `.opencode/commands/crit.md` + `.opencode/skills/crit/SKILL.md` + `.opencode/plugins/crit.ts` (+ registers the plugin in `opencode.jsonc`) | `~/.config/opencode/commands/crit.md` + `~/.agents/skills/crit/SKILL.md` + `~/.config/opencode/plugins/crit.ts` (+ registers the plugin in `~/.config/opencode/opencode.jsonc`) |
| Codex | `crit install codex` | `.agents/skills/crit/SKILL.md` + `.agents/skills/crit-cli/SKILL.md` | `~/.agents/skills/crit/SKILL.md` + `~/.agents/skills/crit-cli/SKILL.md` |
| Codex plugin | `crit install codex-plugin` | `.agents/skills/*` loose `$crit` skills + `.agents/plugins/marketplace.json` + `plugins/crit/` | loose skills and marketplace under `~/.agents/`, plugin under `~/.codex/plugins/crit/` |
| Pi | `crit install pi` | `.pi/skills/crit/SKILL.md` + `.pi/skills/crit-cli/SKILL.md` | `~/.pi/agent/skills/crit/SKILL.md` + `~/.pi/agent/skills/crit-cli/SKILL.md` |
| Qwen Code | `crit install qwen` | `.qwen/skills/crit/SKILL.md` + `.qwen/skills/crit-cli/SKILL.md` | `~/.qwen/skills/crit/SKILL.md` + `~/.qwen/skills/crit-cli/SKILL.md` |
| Hermes | `crit install hermes` | `.hermes/skills/crit/SKILL.md` + `.hermes/skills/crit-cli/SKILL.md` (requires adding `.hermes/skills` to `external_dirs` in `~/.hermes/config.yaml`) | `~/.hermes/skills/crit/SKILL.md` + `~/.hermes/skills/crit-cli/SKILL.md` |
| Windsurf | `crit install windsurf` | `.windsurf/rules/crit.md` | (not supported — Windsurf only allows a single shared `global_rules.md`) |
| Cline | `crit install cline` | `.clinerules/crit.md` | `~/Documents/Cline/Rules/crit.md` (Linux uses `xdg-user-dir DOCUMENTS`; Windows uses `%USERPROFILE%\Documents\Cline\Rules\`) |
| Aider | `crit install aider` | `.crit/aider-conventions.md` + adds entry under `read:` in `.aider.conf.yml` | `~/.crit-conventions.md` + adds entry under `read:` in `~/.aider.conf.yml` |
| Gemini CLI | `crit install gemini` | `.gemini/skills/crit-cli/SKILL.md` + `.gemini/commands/crit.toml` + `.gemini/policies/crit.toml` + `.gemini/settings.json` (merged) | `~/.gemini/skills/crit-cli/SKILL.md` + `~/.gemini/commands/crit.toml` + `~/.gemini/policies/crit.toml` + `~/.gemini/settings.json` (merged) |
| Grok | `crit install grok` | `.grok/skills/crit/SKILL.md` + `.grok/skills/crit-cli/SKILL.md` | `~/.grok/skills/crit/SKILL.md` + `~/.grok/skills/crit-cli/SKILL.md` |

## Plugin marketplace (Claude Code)

For the full experience, install via the plugin marketplace. This gives you:
- A `/crit` slash command for the review loop
- A `crit` skill that auto-activates when working with review files, `crit comment`, `crit pull/push`, etc.

```
claude plugin marketplace add tomasz-tomczyk/crit
claude plugin install crit@crit
```

The marketplace manifest lives at the repo root (`.claude-plugin/marketplace.json`) and points to the plugin files in `integrations/claude-code/`.

### `crit install` vs plugin marketplace

| | `crit install` | Plugin marketplace |
|---|---|---|
| **Scope** | Per-project (committed to repo) | Global (user-wide) |
| **What's installed** | `/crit` skill only | `/crit` skill + `crit-cli` skill |
| **Good for** | Teams — everyone gets the integration | Individual users — works across all projects |
| **Setup** | Run once per project | Install once, works everywhere |

Both approaches give you the `/crit` slash command. The plugin marketplace additionally installs the `crit-cli` skill which auto-teaches the agent about `crit comment`, review file format, `crit pull/push`, and resolution workflow.

## OpenCode plugin: conditional sharing instructions

`crit install opencode` also writes a small TypeScript plugin (`crit.ts`) and registers it in `opencode.jsonc`. The plugin shells out to `crit config` on each chat turn and appends sharing instructions to the system prompt only when `share_url` is set. With `share_url: ""` the sharing block is omitted entirely — useful in environments with strict information-sharing policies, and saves tokens otherwise. opencode auto-loads `.ts` files dropped into the plugin directory, so the registration entry is informational.

## Codex plugin

For Codex, `crit install codex-plugin` installs both:

- loose `.agents/skills/crit` and `.agents/skills/crit-cli` skills, so bare `$crit` works directly
- a local Codex plugin marketplace entry under `.agents/plugins/` or `~/.agents/plugins/`
- plugin files under `plugins/crit/` for project installs or `~/.codex/plugins/crit/` for global installs

The plugin bundles the same skills plus a Codex `Stop` hook that runs `crit plan-hook --mode codex` for proposed-plan review when plugin hooks are enabled.

## What these do

All integrations follow the same pattern:

1. **Pick the review target** — current git changes by default, or an explicit file, plan, PR, or commit range when the user names one
2. **Launch Crit** — the agent runs the matching `crit` command to open the review in your browser
3. **Address feedback** — after review, the agent reads the review file to find your inline comments and revises the target
4. **Continue the review loop** — the agent reruns the printed next-round command until you finish with no unresolved comments

Each integration also teaches the agent about:
- **`crit comment`** — leave inline review comments programmatically without opening the browser
- **review file format** — how to read comments, resolve them with threaded replies
- **`crit pull/push`** — sync reviews with GitHub PRs (push supports `--event approve|request-changes|comment`)
