# Crit Integrations

Drop-in configuration files that teach your AI coding tool to write plans, launch Crit for review, and wait for your feedback before implementing.

Copy the file for your tool into your project.

| Tool | File to copy | Destination in your project |
|------|-------------|----------------------------|
| Claude Code | `claude-code/crit.md` | `.claude/commands/crit.md` |
| Claude Code | `claude-code/CLAUDE.md` (optional) | Append to your `CLAUDE.md` |
| Cursor | `cursor/crit.mdc` | `.cursor/rules/crit.mdc` |
| Windsurf | `windsurf/crit.md` | `.windsurf/rules/crit.md` |
| GitHub Copilot | `github-copilot/copilot-instructions.md` | Append to `.github/copilot-instructions.md` |
| Aider | `aider/CONVENTIONS.md` | Append to your `CONVENTIONS.md` |
| Cline | `cline/crit.md` | `.clinerules/crit.md` |

## What these do

All integrations follow the same pattern:

1. **Plan first** - the agent writes an implementation plan as a markdown file before writing any code
2. **Launch Crit** - the agent runs `crit $PLAN_FILE` to open the plan for review in your browser
3. **Address feedback** - after review, the agent reads the `.review.md` file and revises the plan
4. **Implement after approval** - only after you approve does the agent write code

The Claude Code integration goes further with a `/crit` slash command that automates the full loop: find the plan, launch Crit, read comments, revise, and signal for another round.
