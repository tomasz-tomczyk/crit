# Crit Integrations

Drop-in configuration files that teach your AI coding tool to use Crit for reviewing plans and code changes.

## Quick install

```bash
crit install <tool>     # Install for a specific tool
crit install all        # Install for all supported tools
```

Or copy files manually from this directory into your project.

## Supported tools

### Plugin-based (auto-discovery of commands + skills)

| Tool | Install command | Destination |
|------|----------------|-------------|
| Claude Code | `crit install claude-code` | `.claude/plugins/crit/` |
| Cursor | `crit install cursor` | `.cursor/plugins/crit/` |
| GitHub Copilot | `crit install github-copilot` | `.github/plugins/crit/` |

These tools support plugins. `crit install` creates a plugin with:
- A `/crit` slash command for the review loop
- A `crit` skill that auto-activates when working with `.crit.json`, `crit comment`, `crit pull/push`, etc.

### Rules-based (single config files)

| Tool | Install command | Destination |
|------|----------------|-------------|
| OpenCode | `crit install opencode` | `.opencode/commands/crit.md` + `.opencode/skills/crit/SKILL.md` |
| Windsurf | `crit install windsurf` | `.windsurf/rules/crit.md` |
| Cline | `crit install cline` | `.clinerules/crit.md` |
| Aider | — (copy manually) | Append `aider/CONVENTIONS.md` to your `CONVENTIONS.md` |

## What these do

All integrations follow the same pattern:

1. **Plan first** — the agent writes an implementation plan as a markdown file before writing any code
2. **Launch Crit** — the agent runs `crit $PLAN_FILE` to open the plan for review in your browser
3. **Address feedback** — after review, the agent reads `.crit.json` to find your inline comments and revises the plan
4. **Implement after approval** — only after you approve does the agent write code

Each integration also teaches the agent about:
- **`crit comment`** — leave inline review comments programmatically without opening the browser
- **`.crit.json` format** — how to read comments, resolve them with `resolution_note` and `resolution_lines`
- **`crit pull/push`** — sync reviews with GitHub PRs

## Install methods

### 1. Plugin marketplace (Claude Code, Cursor)

Installs globally — works across all projects without per-repo setup.

**Claude Code:**
```
/plugin marketplace add tomasz-tomczyk/crit
/plugin install crit
```

**Cursor:**
Add `tomasz-tomczyk/crit` as a marketplace source in Cursor settings, then install the `crit` plugin.

The marketplace manifests live at the repo root (`.claude-plugin/marketplace.json`, `.cursor-plugin/marketplace.json`) and point to the plugin files in `integrations/claude-code/` and `integrations/cursor/` respectively.

### 2. Per-project install (`crit install`)

Installs into the current project directory. Good for teams — files are committed to the repo so everyone gets the integration.

```bash
crit install claude-code   # or: cursor, opencode, windsurf, github-copilot, cline
crit install all           # install all integrations at once
```

Safe to re-run — existing files are skipped (use `--force` to overwrite).

### 3. Manual copy

Copy files from this directory into the appropriate locations in your project. See the tables above for destination paths.
