# Crit

Your AI agent just generated a plan. Before you let it write 2,000 lines of code, you should review that plan.

Crit opens any markdown file in your browser as a reviewable document. Click to select lines, leave inline comments, then hand the annotated result back to your agent. Repeat until the plan is right.

## Workflow

Crit fits into the plan-then-implement cycle that AI coding agents work best with. The review stays local. No account, no upload, no waiting for a build.

```bash
# 1. Ask your agent to write a plan (example using Claude Code)
claude "Write an implementation plan for the auth service" > auth-plan.md

# 2. Review it
crit auth-plan.md
# → Browser opens with the plan rendered and commentable
# → Drag to select lines, leave comments
# → Click "Finish Review". A ready-made agent prompt is copied to clipboard

# 3. Hand the review back
# Paste the clipboard into your agent, or:
claude "I've reviewed auth-plan.md. See auth-plan.review.md for comments."

# 4. Your agent edits the file and signals it's done
# → crit go $PORT triggers a new round with a diff of what changed
# → Review the diff, leave more comments, repeat
```

One command. Browser opens. You review. Your agent gets structured feedback. When it's done editing, it runs `crit go <port>` to signal completion. Crit starts a new round and shows a diff of what changed.

Works with Claude Code, Cursor, GitHub Copilot, Aider, or any agent that reads files. There's nothing to configure. Your agent gets a markdown file with your comments in it.

### Output

Crit generates a structured `.review.md` file with your comments interleaved as blockquotes at the exact lines they reference.

| File                  | Purpose                                                                         |
| --------------------- | ------------------------------------------------------------------------------- |
| `plan.review.md`      | Original plan + comments as blockquotes. Hand to your AI agent                  |
| `.plan.comments.json` | Comment state and session data. Your agent marks comments resolved in this file |

## Demo

A 2-minute walkthrough: reviewing a plan, leaving inline comments, handing off to an agent.

[![Crit demo](https://github.com/user-attachments/assets/dec9c069-9a99-4254-9b05-6d8db30820ed)](https://www.youtube.com/watch?v=w_Dswm2Ft-o)

## Install

### Homebrew (macOS / Linux)

```bash
brew install tomasz-tomczyk/tap/crit
```

### Go

```bash
go install github.com/tomasz-tomczyk/crit@latest
```

### Nix

```bash
nix profile install github:tomasz-tomczyk/crit
```

Or in a `flake.nix`:

```nix
inputs.crit.url = "github:tomasz-tomczyk/crit";
```

### Download Binary

Grab the latest binary for your platform from [Releases](https://github.com/tomasz-tomczyk/crit/releases).

## Features

### Inline comments: single lines and ranges

Click a line number to comment. Drag to select a range. Comments are rendered inline after their referenced lines, just like a GitHub PR review.

![Simple comments](images/simple-comments.gif)

The generated `.review.md` places each comment as a blockquote immediately after the referenced lines:

```markdown
> **Note**: This plan covers the MVP scope. SAML integration is deferred to Phase 2
> unless the enterprise sales team escalates priority.

> **[REVIEW COMMENT — Lines 8-9]**: We should definitely defer SAML!

...

### Components

1. **Auth API** — handles login, logout, token refresh
2. **Token Service** — JWT issuance and validation
3. **Provider Adapters** — pluggable OAuth2/SAML providers
4. **Session Store** — Redis-backed session management

> **[REVIEW COMMENT — Lines 16-21]**: Can you be more specific here?
```

### Suggestion mode

Select lines and use "Insert suggestion" to pre-fill the comment with the original text. Edit it to show exactly what the replacement should look like. Your agent gets a concrete before/after.

![Insert suggestion](images/suggestion.gif)

### Round-to-round diff

After your agent edits the file, Crit shows a split or unified diff between the previous and current version. Resolved comments are marked. Open ones stay visible so you can verify nothing was skipped.

### Mermaid diagrams

Architecture diagrams in fenced ` ```mermaid ` blocks render inline. You can comment on the diagram source just like any other block.

![Mermaid diagram](images/mermaid.png)

### Finish review: prompt copied to clipboard

When you click "Finish Review", Crit collects all your comments, formats them into a structured prompt, and copies it to your clipboard. Paste directly into your agent.

![Agent prompt](images/prompt.png)

### Share for async review

Want a second opinion before handing off to the agent? The Share button uploads your review to [crit.live](https://crit.live) and gives you a public URL anyone can open in a browser, no install needed. Each reviewer's comments are color-coded by author. Unpublish anytime.

### Everything else

- Real-time `.review.md` written on every keystroke (200ms debounce)
- Live file watching via SSE. Browser reloads when the source changes
- Syntax highlighting inside code blocks, per-line commentable
- Dark/light/system theme with persistence
- Draft autosave. Close your browser mid-review and pick up where you left off
- Vim keybindings (`j`/`k` navigate, `c` comment, `f` finish, `?` for full reference)
- Single binary. No daemon, no dependencies, no install friction
- Runs on `127.0.0.1`. Your files stay local unless you explicitly share

## Agent Integrations

Crit ships with drop-in configuration files for popular AI coding tools. Each one teaches your agent to write a plan, launch `crit` for review, and wait for your feedback before implementing.

| Tool | Setup |
|------|-------|
| **Claude Code** | Copy `integrations/claude-code/crit.md` to `.claude/commands/crit.md` for the `/crit` slash command. Optionally append the `CLAUDE.md` snippet to your project's `CLAUDE.md` |
| **Cursor** | Copy `integrations/cursor/crit.mdc` to `.cursor/rules/crit.mdc` |
| **Windsurf** | Copy `integrations/windsurf/crit.md` to `.windsurf/rules/crit.md` |
| **GitHub Copilot** | Append `integrations/github-copilot/copilot-instructions.md` to `.github/copilot-instructions.md` |
| **Aider** | Append `integrations/aider/CONVENTIONS.md` to your `CONVENTIONS.md` |
| **Cline** | Copy `integrations/cline/crit.md` to `.clinerules/crit.md` |

See [`integrations/`](integrations/) for the full files and details.

### Claude Code `/crit` command

The Claude Code integration includes a slash command that automates the full loop:

```
/crit              # Auto-detects the current plan file
/crit my-plan.md   # Review a specific file
```

It launches Crit, waits for your review, reads your comments, revises the plan, and signals Crit for another round.

## Usage

```bash
# Review a markdown file (opens browser automatically)
crit plan.md

# Specify a port
crit -p 3000 plan.md

# Don't auto-open browser
crit --no-open plan.md

# Custom output directory for .review.md
crit -o /tmp plan.md
```

## Environment Variables

| Variable               | Description                                                                                |
| ---------------------- | ------------------------------------------------------------------------------------------ |
| `CRIT_SHARE_URL`       | Override the Share button URL (defaults to crit.live, useful for self-hosted or local dev) |
| `CRIT_NO_UPDATE_CHECK` | Set to any value to disable the update check on startup                                    |

## Build from Source

Requires Go 1.25+ (install via [asdf](https://asdf-vm.com/), Homebrew, or [go.dev](https://go.dev/dl/)):

```bash
# Clone and build
git clone https://github.com/tomasz-tomczyk/crit.git
cd crit
go build -o crit .

# Optionally move to your PATH
mv crit /usr/local/bin/
```

### Cross-compile

```bash
make build-all
# Outputs to dist/:
#   crit-darwin-arm64
#   crit-darwin-amd64
#   crit-linux-amd64
#   crit-linux-arm64
```

## Acknowledgments

Crit embeds the following open-source libraries:

- [markdown-it](https://github.com/markdown-it/markdown-it): Markdown parser
- [highlight.js](https://github.com/highlightjs/highlight.js): Syntax highlighting
- [Mermaid](https://github.com/mermaid-js/mermaid): Diagram rendering
