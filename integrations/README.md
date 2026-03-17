# Crit Integrations

Drop-in configuration files that teach your AI coding tool to use Crit for reviewing plans and code changes.

## Quick install

```bash
crit install <tool>     # Install for a specific tool
crit install all        # Install for all supported tools
```

This installs a `/crit` slash command into your project. Safe to re-run — existing files are skipped (use `--force` to overwrite).

| Tool | Install command | Destination |
|------|----------------|-------------|
| Claude Code | `crit install claude-code` | `.claude/commands/crit.md` |
| Cursor | `crit install cursor` | `.cursor/commands/crit.md` |
| GitHub Copilot | `crit install github-copilot` | `.github/prompts/crit.prompt.md` |
| OpenCode | `crit install opencode` | `.opencode/commands/crit.md` + `.opencode/skills/crit/SKILL.md` |
| Windsurf | `crit install windsurf` | `.windsurf/rules/crit.md` |
| Cline | `crit install cline` | `.clinerules/crit.md` |
| Aider | — (copy manually) | Append `aider/CONVENTIONS.md` to your `CONVENTIONS.md` |

## Plugin marketplace (Claude Code)

For the full experience, install via the plugin marketplace. This gives you:
- A `/crit` slash command for the review loop
- A `crit` skill that auto-activates when working with `.crit.json`, `crit comment`, `crit pull/push`, etc.

```
/plugin marketplace add tomasz-tomczyk/crit
/plugin install crit
```

The marketplace manifest lives at the repo root (`.claude-plugin/marketplace.json`) and points to the plugin files in `integrations/claude-code/`.

### `crit install` vs plugin marketplace

| | `crit install` | Plugin marketplace |
|---|---|---|
| **Scope** | Per-project (committed to repo) | Global (user-wide) |
| **What's installed** | `/crit` command only | `/crit` command + `crit` skill |
| **Good for** | Teams — everyone gets the integration | Individual users — works across all projects |
| **Setup** | Run once per project | Install once, works everywhere |

Both approaches give you the `/crit` slash command. The plugin marketplace additionally installs the `crit` skill which auto-teaches the agent about `crit comment`, `.crit.json` format, `crit pull/push`, and resolution workflow.

## What these do

All integrations follow the same pattern:

1. **Plan first** — the agent writes an implementation plan as a markdown file before writing any code
2. **Launch Crit** — the agent runs `crit $PLAN_FILE` to open the plan for review in your browser
3. **Address feedback** — after review, the agent reads `.crit.json` to find your inline comments and revises the plan
4. **Implement after approval** — only after you approve does the agent write code

Each integration also teaches the agent about:
- **`crit comment`** — leave inline review comments programmatically without opening the browser
- **`.crit.json` format** — how to read comments, resolve them with threaded replies
- **`crit pull/push`** — sync reviews with GitHub PRs
