# Crit

[![CI](https://github.com/tomasz-tomczyk/crit/actions/workflows/test.yml/badge.svg)](https://github.com/tomasz-tomczyk/crit/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/release/tomasz-tomczyk/crit.svg)](https://github.com/tomasz-tomczyk/crit/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/tomasz-tomczyk/crit)](https://goreportcard.com/report/github.com/tomasz-tomczyk/crit)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Reviewing agent output in a terminal is painful. You can't point at a specific line and say "change this." When your agent updates the file, you re-read the whole thing to figure out what changed.

Crit opens your file in a browser with GitHub-style inline comments. Leave feedback, hit Finish, and your agent is notified automatically. When the agent edits, Crit shows a diff between rounds - you see exactly what it addressed.

Works with Claude Code, Cursor, GitHub Copilot, Aider, Cline, Windsurf and any other agent.

## Why Crit

- **Browser UI, not terminal.** A persistent tab with rendered markdown and visual diffs. No tmux, no TUI.
- **Single binary, zero dependencies.** `brew install` and you're done. No daemon, no Docker, no MCP.
- **Round-to-round diffs.** See exactly what your agent changed between iterations. Previous comments show as resolved or still open.
- **Works with any agent.** Not locked to one editor or AI provider.

![Crit review UI](images/demo-overview.png)

## Install

```bash
brew install tomasz-tomczyk/tap/crit
```

Also available via [Go, Nix, or binary download](#other-install-methods).

## Demo

A 5-minute walkthrough of plan review and branch review.

[![Crit demo](https://github.com/user-attachments/assets/dec9c069-9a99-4254-9b05-6d8db30820ed)](https://www.youtube.com/watch?v=XRjkRpXuLJc)

## Usage

```bash
crit                          # auto-detect changed files in your repo
crit plan.md                  # review a specific file
crit plan.md api-spec.md      # review multiple files
```

When you finish a review, Crit writes `.crit.json` - structured comment data your agent reads and acts on. Add it to your `.gitignore`:

```bash
echo '.crit.json' >> .gitignore
```

## Features

### File review

Pass specific files to review them directly: `crit plan.md api-spec.md`. Markdown files render as formatted documents with per-line commenting. Code files show as syntax-highlighted source. Both support the same inline comment workflow and multi-round iteration.

### Git review

Run `crit` with no arguments. Crit auto-detects changed files in your repo and opens them as syntax-highlighted git diffs. A file tree on the left shows every file with its status (added, modified, deleted) and comment counts. Toggle between split and unified diff views.

![Crit review for your branch](images/git-mode.png)

### Round-to-round diff

After your agent edits the file, Crit shows a split or unified diff of what changed - toggle it in the header.

#### Split view

![Round-to-round diff - split view](images/diff-split.png)

#### Unified view

![Round-to-round diff - unified view](images/diff-unified.png)

### Inline comments: single lines and ranges

Click a line number to comment. Drag to select a range. Comments are rendered inline after their referenced lines, just like a GitHub PR review.

![Simple comments](images/simple-comments.gif)

### Suggestion mode

Select lines and use "Insert suggestion" to pre-fill the comment with the original text. Edit it to show exactly what the replacement should look like. Your agent gets a concrete before/after.

![Insert suggestion](images/suggestion.gif)

### Finish review: agent notified automatically

When you click "Finish Review", Crit writes `.crit.json` and notifies your agent via `crit listen`. If your agent was listening, it picks up the prompt automatically — no copy-paste needed. A fallback "Copy prompt" button is available if the agent wasn't listening.

![Agent prompt](images/prompt.png)

### Programmatic comments

AI agents can use `crit comment` to add inline review comments without opening the browser UI or constructing JSON manually:

```bash
crit comment src/auth.go:42 'Missing null check'
crit comment src/handler.go:15-28 'Error handling issue'
crit comment --output /tmp/reviews src/auth.go:42 'comment'  # custom output dir
crit comment --clear   # remove .crit.json
```

Comments are appended to `.crit.json` - created automatically if it doesn't exist.

### Mermaid diagrams

Architecture diagrams in fenced ` ```mermaid ` blocks render inline. You can comment on the diagram source just like any other block.

![Mermaid diagram](images/mermaid.png)

### Share for Async Review

Want a second opinion before handing off to the agent? Click the Share button to upload your review and get a public URL anyone can open in a browser, no install needed. Each reviewer's comments are color-coded by author. Unpublish anytime.

You can also share directly from the CLI without starting the browser UI:

```bash
crit share plan.md                    # share files and print the URL
crit share plan.md --qr               # also print a QR code in the terminal
crit unpublish                        # remove the shared review
```

Sharing uses [crit.md](https://crit.md) by default. To self-host, deploy [`crit-web`](https://github.com/tomasz-tomczyk/crit-web) and point `CRIT_SHARE_URL` (or `--share-url`, or `share_url` in config) at your instance. Set `share_url` to `""` to disable sharing entirely.

### GitHub PR Sync

Crit can sync review comments bidirectionally with GitHub PRs. Requires the [GitHub CLI](https://cli.github.com) (`gh`) to be installed and authenticated.

#### Pull comments from a PR

```bash
crit pull              # auto-detects PR from current branch
crit pull 42           # explicit PR number
```

#### Push comments to a PR

```bash
crit push                          # auto-detects PR from current branch
crit push --dry-run                # preview without posting
crit push --message "Round 2"      # add a top-level review comment
crit push 42                       # explicit PR number
```

### Everything else

- **Draft autosave.** Close your browser mid-review and pick up exactly where you left off.
- **Vim keybindings.** `j`/`k` to navigate, `c` to comment, `Shift+F` to finish. `?` for the full reference.
- **Concurrent reviews.** Each instance runs on its own port - review multiple plans at once.
- **Syntax highlighting.** Code blocks are highlighted and split per-line, so you can comment on individual lines inside a fence.
- **Live file watching.** The browser reloads automatically when the source file changes.
- **Real-time output.** `.crit.json` is written on every comment change (200ms debounce), so your agent always has the latest review state.
- **Dark/light/system theme.** Three-button pill in the header, persisted to localStorage.
- **Local by default.** Server binds to `127.0.0.1`. Your files stay on your machine unless you explicitly share.

## Agent Integrations

Crit ships with plugins and configuration files for popular AI coding tools. Each one teaches your agent to write a plan, launch `crit` for review, and wait for your feedback before implementing.

### Per-project install

The fastest way to get started. Installs a `/crit` slash command plus any integration companion files available for that tool (for example `SKILL.md` files) into your project:

```bash
crit install claude-code   # or: cursor, opencode, windsurf, github-copilot, cline
crit install all           # install all integrations at once
```

Safe to re-run — existing files are skipped (use `--force` to overwrite). Good for teams since the files are committed to the repo.

### Plugin install (Claude Code)

For the full experience — installs globally with a `/crit` command plus a `crit` skill that auto-activates when your agent works with `.crit.json`, `crit comment`, `crit pull/push`, etc:

```
/plugin marketplace add tomasz-tomczyk/crit
/plugin install crit
```

See [`integrations/`](integrations/) for all install methods and details.

### `/crit` command

Claude Code, Cursor, OpenCode, and GitHub Copilot support a `/crit` slash command that automates the full review loop:

```
/crit              # Auto-detects the current plan file
/crit my-plan.md   # Review a specific file
```

It launches Crit, waits for your review, reads your comments, revises the plan, and signals Crit for another round.

## Configuration

Crit supports persistent configuration via JSON files so you don't have to pass the same flags every time.

| File                  | Scope   | Location                                         |
| --------------------- | ------- | ------------------------------------------------ |
| `~/.crit.config.json` | Global  | Applies to all projects                          |
| `.crit.config.json`   | Project | Repo root (from `git rev-parse --show-toplevel`) |

Project config overrides global. CLI flags and env vars override both.

```bash
crit config --generate > ~/.crit.config.json   # scaffold a starter config file
crit config                                    # view resolved config (merged global + project)
crit config --help                             # document all config keys
```

### Example

```json
{
  "port": 0,
  "base_branch": "main",
  "no_open": false,
  "share_url": "https://crit.md",
  "quiet": false,
  "output": "",
  "author": "John",
  "ignore_patterns": [".crit.json"]
}
```

All keys are optional - omit any you don't need.

### Ignore patterns

Patterns from global and project configs are merged. Supported syntax:

| Pattern             | Matches                                         |
| ------------------- | ----------------------------------------------- |
| `*.lock`            | Files ending in `.lock` anywhere in tree        |
| `vendor/`           | All files under `vendor/`                       |
| `package-lock.json` | Exact filename anywhere in tree                 |
| `generated/*.pb.go` | Path prefix with glob (`filepath.Match` syntax) |

Use `--no-ignore` to temporarily bypass all patterns:

```bash
crit --no-ignore
```

### Environment variables

| Variable               | Description                                                                |
| ---------------------- | -------------------------------------------------------------------------- |
| `CRIT_SHARE_URL`       | Enable the Share button (e.g. `https://crit.md` or a self-hosted instance) |
| `CRIT_PORT`            | Default port for the local server                                          |
| `CRIT_NO_UPDATE_CHECK` | Set to any value to disable the update check on startup                    |

## Other Install Methods

### Build from Source

Requires Go 1.26+:

```bash
git clone https://github.com/tomasz-tomczyk/crit.git
cd crit
go build -o crit .
mv crit /usr/local/bin/
```

### Go

```bash
go install github.com/tomasz-tomczyk/crit@latest
```

### Nix

```bash
nix run github:tomasz-tomczyk/crit -- --help
```

Or add it to a `flake.nix`:

```nix
inputs.crit.url = "github:tomasz-tomczyk/crit";
```

### Download Binary

Grab the latest binary for your platform from [Releases](https://github.com/tomasz-tomczyk/crit/releases).

## Acknowledgements

Crit embeds the following open-source libraries:

- [markdown-it](https://github.com/markdown-it/markdown-it): Markdown parser
- [highlight.js](https://github.com/highlightjs/highlight.js): Syntax highlighting
- [Mermaid](https://github.com/mermaid-js/mermaid): Diagram rendering
