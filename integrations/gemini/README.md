# Crit — Gemini CLI Integration

Drop-in configuration files that teach Gemini CLI to use Crit for reviewing plans and code changes.

## What's included

| File | Purpose |
|------|---------|
| `skills/crit/SKILL.md` | Main review skill — runs the interactive review loop (`/crit` command) |
| `skills/crit-cli/SKILL.md` | CLI reference skill — `crit comment`, `crit pull/push`, `crit share`, review file format |
| `commands/crit-review.toml` | `/crit-review` slash command that activates the review skill |
| `hooks/settings-snippet.json` | Hook that intercepts plan mode exit and runs `crit plan-hook` for inline plan review |
| `hooks/policy.toml` | Auto-allows `exit_plan_mode` without confirmation (browser UI is the sole gate) |

## Install

```bash
crit install gemini
```

This installs the skills and command to your project directory. For a global install (available across all projects):

```bash
cd ~ && crit install gemini
```

## Manual setup

If you prefer to install manually:

1. **Skills** — copy to your project or home directory:
   ```
   skills/crit/SKILL.md      → .gemini/skills/crit/SKILL.md
   skills/crit-cli/SKILL.md  → .gemini/skills/crit-cli/SKILL.md
   ```
   Global: `~/.gemini/skills/crit/SKILL.md` and `~/.gemini/skills/crit-cli/SKILL.md`

2. **Command** — copy to your project or home directory:
   ```
   commands/crit-review.toml → .gemini/commands/crit-review.toml
   ```
   Global: `~/.gemini/commands/crit-review.toml`

3. **Hook** — merge `hooks/settings-snippet.json` into `~/.gemini/settings.json`. Add the `hooks` key if it doesn't exist, or merge the `BeforeTool` array into the existing `hooks` object.

4. **Policy** — copy to your project or home directory:
   ```
   hooks/policy.toml → .gemini/policies/crit.toml
   ```
   Global: `~/.gemini/policies/crit.toml`

## Usage

Once installed, use `/crit-review` in Gemini CLI to start a review loop. The agent will:

1. Run `crit` to open your browser for inline commenting
2. Wait for you to click "Finish Review"
3. Read your comments and address each one
4. Loop until you approve with no remaining comments
