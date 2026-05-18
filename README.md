# Crit

[![CI](https://github.com/tomasz-tomczyk/crit/actions/workflows/test.yml/badge.svg)](https://github.com/tomasz-tomczyk/crit/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/tomasz-tomczyk/crit/graph/badge.svg)](https://codecov.io/gh/tomasz-tomczyk/crit)
[![Release](https://img.shields.io/github/release/tomasz-tomczyk/crit.svg)](https://github.com/tomasz-tomczyk/crit/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/tomasz-tomczyk/crit)](https://goreportcard.com/report/github.com/tomasz-tomczyk/crit)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A browser-based review UI for AI agent output. Point at the line. Tell the agent.

Your agent just touched 14 files. In the terminal you're scrolling through diffs hoping nothing broke. Crit opens those changes in a browser — click line 47, type "this drops the refresh token", and the agent fixes it. You see exactly what changed, round by round.

![Crit review UI](images/demo-overview.png)

## Four review modes

Agents don't just write code. They write plans, generate HTML pages, modify running apps. Each output needs a different review surface — terminal diffs work for none of them.

- **Plans & docs** — `crit plan.md` renders markdown in the browser. Comment on the section that's wrong, not the whole document.
- **Code** — `crit` auto-detects branch changes and shows syntax-highlighted diffs. Like a PR review, but instant and local.
- **Live** — `crit http://localhost:3000` proxies your running app into a review surface. Click the misaligned button, pin a comment to it.
- **Preview** — `crit landing.html` renders a static HTML artifact in an iframe. Pin comments to elements, no dev server needed.

Single binary. Local by default. No account, no config, no dependencies.

## Install

```bash
brew install crit
```

Also available via [Go, Nix, or binary download](#other-install-methods).

## Agent Integrations

Works with Claude Code, Cursor, GitHub Copilot, OpenCode, Codex, Gemini, Qwen, Hermes, Windsurf, Cline, Grok, Aider, and Pi — any agent that can read a file and run a command. See [`integrations/`](integrations/) for all install methods and details.

### Plugin install (Claude Code)

For the full experience - installs globally with a `/crit` command plus a `crit` skill that auto-activates when your agent works with review files, `crit comment`, `crit pull/push`, etc:

```
claude plugin marketplace add tomasz-tomczyk/crit
claude plugin install crit@crit
```

### `/crit` command

Most integrations include a `/crit` slash command that automates the full review loop. It launches Crit, waits for your review; your agent acts on the feedback and you go back and forth until the work is approved.

## Demo

A 2-minute walkthrough of plan review and branch review.

[![Crit demo](images/video-thumbnail.png)](https://www.youtube.com/watch?v=LHwfdvePf5A)

## Usage

The recommended way is to use `/crit` command with your agent after any piece of work - whether it wrote a plan or made some code changes. You can however, launch it in your terminal by yourself and paste the prompt when you finish to your agent.

```bash
crit                              # auto-detect changed files in your repo
crit plan.md                      # review a specific file
crit plan.md api-spec.md          # review multiple files
crit http://localhost:3000        # review a running dev server
crit landing.html                 # review a static HTML file
```

```bash
crit status                       # show review file path and daemon status
crit cleanup                      # delete stale review files
```

## Features

### Round-to-round diff

After your agent edits the file, Crit shows a split or unified diff of what changed - toggle it in the header.

#### Split view

![Round-to-round diff - split view](images/diff-split.png)

#### Unified view

![Round-to-round diff - unified view](images/diff-unified.png)

### Inline comments: single lines and ranges

Click a line number to comment. Drag to select a range. Comments are rendered inline after their referenced lines, just like a GitHub PR review.

![Simple comments](images/simple-comments.gif)

### Programmatic comments

AI agents can use `crit comment` to add inline review comments without opening the browser UI or constructing JSON manually:

```bash
crit comment src/auth.go:42 'Missing null check'
crit comment src/handler.go:15-28 'Error handling issue'
crit comment --output /tmp/reviews src/auth.go:42 'comment'  # custom output dir
crit comment --clear   # remove the review file
```

Comments are appended to the review file (stored in `~/.crit/reviews/`) and created automatically if it doesn't exist. Run `crit status` to see the active review file path.

### Share for Async Review

Want a second opinion before handing off to the agent? Click the Share button to upload your review and get a public URL anyone can open in a browser, no install needed. Each reviewer's comments are color-coded by author. Unpublish anytime.

You can also share directly from the CLI without starting the browser UI:

```bash
crit share plan.md                    # share files and print the URL
crit share plan.md --qr               # also print a QR code in the terminal
crit share plan.md --org acme         # share under an organization
crit share plan.md --org acme --visibility unlisted  # org share with explicit visibility
crit unpublish                        # remove the shared review
```

When sharing under an org, visibility defaults to `organization` (members only). Override with `--visibility` (`organization`, `unlisted`, or `public`). The browser UI shows an org picker when you're signed in and belong to an organization.

Sharing uses [crit.md](https://crit.md) by default. To self-host, deploy [`crit-web`](https://github.com/tomasz-tomczyk/crit-web) and point `CRIT_SHARE_URL` (or `--share-url`, or `share_url` in config) at your instance. Set `share_url` to `""` to disable sharing entirely.

If your self-hosted `crit-web` sits behind an SSO reverse proxy that the terminal can't authenticate against, set `proxy_auth: true` in your `~/.crit.config.json` (this option is config-only and global-only — it's a property of the deployment, not a per-invocation choice, so there's no flag or env var). Browser-driven Share / Pull / Re-share / Unpublish then route through a popup window where the proxy can complete its interactive auth flow. Terminal `crit share`, `crit fetch`, and `crit unpublish` remain unavailable behind SSO — use the browser UI buttons.

#### Authentication

You can share anonymously or you can create a free crit.md account (using GitHub oAuth). To authenticate with crit-web (for sharing and other features that require an account):

```bash
crit auth login                    # opens browser to log in
crit auth whoami                   # show current user info
crit auth logout                   # log out and revoke token
```

`crit auth login` uses the OAuth Device Flow - it opens your browser, you confirm, and the CLI receives a token automatically. The token is stored in your global config (`~/.crit.config.json`).

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

### Send to agent (experimental)

Click "Send now" on any comment during a review to get an AI agent response in real-time. This feature only appears when `agent_cmd` is configured.
The agent reads the comment context, addresses it (editing code if needed), and replies
inline - all while you continue reviewing.

![Send to agent](images/live-mode.png)

Configure in `~/.crit.config.json` (global config only):

```json
{
  "agent_cmd": "claude --dangerously-skip-permissions -p"
}
```

> **Security note:** `agent_cmd` is read exclusively from your global `~/.crit.config.json`. Project-level `.crit.config.json` files cannot set it. This prevents a malicious repository from executing arbitrary commands when you trigger "Send to agent".

#### Permission modes

Agents need tool permissions to edit files on your behalf. How you grant them depends on your trust level:

| Mode             | Command                                                   | What the agent can do                                                                       |
| ---------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Full access      | `claude --dangerously-skip-permissions -p`                | Read, write, and run any tool. Simplest option - recommended for trusted repos.             |
| Selective access | `claude --allowedTools Edit,Read,Bash,Write,Glob,Grep -p` | Only the listed tools are permitted. Good middle ground.                                    |
| No permissions   | `claude -p`                                               | The agent can respond to comments but **cannot edit files**. Useful for Q&A-only workflows. |

#### How it works

1. The agent receives the comment text, quoted text (if text was selected), file path, and line range on **stdin**.
2. The agent's **stdout** is captured and posted as a reply to the comment automatically.
3. If the agent edits files, Crit detects the changes via **file watching** and updates the UI.

#### Live threads

After the first agent interaction, the comment becomes a **live thread**:

- Further replies you post in the thread are automatically sent to the agent - no need to click "Send to agent" again.
- The agent sees the **full conversation history**, so it can build on previous context.
- Live threads show a ⚡ **live** badge and green glow - the agent will respond immediately to further replies.

#### Supported agents

| Agent                 | `agent_cmd` value        |
| --------------------- | ------------------------ |
| Claude Code           | `claude -p`              |
| OpenCode              | `opencode ask`           |
| Cline                 | `cline --pipe`           |
| Aider                 | `aider --message-file -` |
| Cursor (experimental) | `cursor --pipe`          |

> **Tip:** Claude Code still prompts for permission in `-p` mode. To let it edit files freely, use `claude --dangerously-skip-permissions -p` instead. The other agents already operate without permission prompts in their pipe/non-interactive modes.
>
> You can also specify a model with `--model` (e.g. `claude --model sonnet -p`).

### Everything else

- **Per-branch review isolation.** Each branch gets its own review file — switch branches freely without losing comments. Review data lives in `~/.crit/reviews/`, not your repo.
- **Draft autosave.** Close your browser mid-review and pick up exactly where you left off.
- **Vim keybindings.** `j`/`k` to navigate, `c` to comment, `Shift+F` to finish. `?` for the full reference.
- **Concurrent reviews.** Each instance runs on its own port - review multiple plans at once.
- **Syntax highlighting.** Code blocks are highlighted and split per-line, so you can comment on individual lines inside a fence.
- **Live file watching.** The browser reloads automatically when the source file changes.
- **Dark/light/system theme.** Three-button pill in the header, persisted to localStorage.
- **Local by default.** Server binds to `127.0.0.1`. Your files stay on your machine unless you explicitly share. (Override with `--host` / `CRIT_HOST` / `host` config key — e.g. `0.0.0.0` to expose on your LAN. No auth, so it's an explicit opt-in.)
- **Collapsing generated files.** Honors `linguist-generated` in `.gitattributes` — matching files appear collapsed by default.
- **No analytics or tracking.** Crit collects zero telemetry. No usage stats, no crash reports, no phone-home. If we ever add anonymous usage statistics in the future, they will be explicitly opt-in.
- **Update check.** On startup, Crit makes one network request to check for a newer version and prints a notice if one is available. Set `CRIT_NO_UPDATE_CHECK=1` to disable it.

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
```

### Config keys

All keys are optional — omit any you don't need.

| Key                    | Type     | Default                    | Description                                                                                                                                                                             |
| ---------------------- | -------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `port`                 | int      | `0` (random)               | Port for the local server. `0` picks a random available port.                                                                                                                           |
| `host`                 | string   | `"127.0.0.1"`              | Listen host. Set to `"0.0.0.0"` to expose the server on your LAN. There is no auth, so any non-loopback bind is an explicit opt-in.                                                     |
| `no_open`              | bool     | `false`                    | Don't auto-open the browser when starting a review.                                                                                                                                     |
| `quiet`                | bool     | `false`                    | Suppress terminal status output.                                                                                                                                                        |
| `output`               | string   | repo root or file dir      | Output directory for review files. Reviews are stored in `~/.crit/reviews/` by default.                                                                                                 |
| `author`               | string   | VCS user name              | Author name shown on comments. Falls back to your configured VCS user name.                                                                                                            |
| `base_branch`          | string   | auto-detected              | Base branch to diff against (e.g. `"main"`, `"develop"`). Overrides auto-detection.                                                                                                     |
| `ignore_patterns`      | string[] | `[".crit/"]` | File patterns to exclude from git-mode file lists. Global and project patterns are merged.                                                                                              |
| `cleanup_on_approve`   | bool     | `true`                     | Automatically delete the review file when you approve with no unresolved comments. Set to `false` to preserve review history.                                                           |
| `no_update_check`      | bool     | `false`                    | Don't check for new versions on startup.                                                                                                                                                |
| `no_integration_check` | bool     | `false`                    | Skip the integration config freshness check on startup.                                                                                                                                 |
| `vcs`                  | string   | auto-detected              | Preferred VCS backend: `"git"`, `"sl"`, or `"jj"`. When set, crit uses this VCS instead of auto-detecting. Falls back to git if the configured VCS isn't available. Can also be set via `--vcs` CLI flag (flag takes precedence over config). |

### Global-only config keys

These keys can only be set in `~/.crit.config.json` (global). Project-level `.crit.config.json` cannot override them — this prevents a malicious repository from hijacking the agent command or redirecting share requests.

| Key                    | Type     | Default                    | Description                                                                                                                                                                             |
| ---------------------- | -------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent_cmd`            | string   | `""`                       | Shell command for "Send to agent" (e.g. `"claude -p"`). See [Send to agent](#send-to-agent-experimental). |
| `auth_token`           | string   | `""`                       | Authentication token for crit.md. Set automatically by `crit auth login`. |
| `share_url`            | string   | `"https://crit.md"`        | Base URL of the share service. Set to `""` to disable sharing entirely. Self-host with [`crit-web`](https://github.com/tomasz-tomczyk/crit-web). |
| `share_consented`      | bool     | `false`                    | Written automatically to `true` after you confirm the first-time share prompt. Reset to `false` to see the prompt again. Not used when `share_url` is a custom (self-hosted) URL. |
| `proxy_auth`           | bool     | `false`                    | When `true`, share / pull / unpublish / re-share use the browser popup relay instead of the local Go server contacting crit-web directly. Use when crit-web is behind an SSO reverse proxy that the terminal cannot authenticate against. No flag or env var — this is a property of the deployment, not a per-invocation choice. |

### CLI flags

| Flag            | Short | Equivalent config key | Description                            |
| --------------- | ----- | --------------------- | -------------------------------------- |
| `--port`        | `-p`  | `port`                | Port to listen on                      |
| `--host`        |       | `host`                | Listen host (default `127.0.0.1`)      |
| `--no-open`     |       | `no_open`             | Don't auto-open browser                |
| `--share-url`   |       | `share_url`           | Share service URL                      |
| `--output`      | `-o`  | `output`              | Output directory for review files      |
| `--quiet`       | `-q`  | `quiet`               | Suppress status output                 |
| `--base-branch` |       | `base_branch`         | Base branch to diff against            |
| `--vcs`         |       | `vcs`                 | VCS backend (`git`, `sl`, or `jj`)     |
| `--no-ignore`   |       |                       | Temporarily bypass all ignore patterns |
| `--version`     | `-v`  |                       | Print version and exit                 |

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

| Variable                    | Description                                       |
| --------------------------- | ------------------------------------------------- |
| `CRIT_PORT`                 | Default port for the local server                 |
| `CRIT_HOST`                 | Listen host (default `127.0.0.1`)                 |
| `CRIT_SHARE_URL`            | Override the share service URL                    |
| `CRIT_AUTH_TOKEN`           | Override the auth token (skips `crit auth login`) |
| `CRIT_NO_UPDATE_CHECK`      | Disable the update check on startup               |
| `CRIT_NO_INTEGRATION_CHECK` | Skip integration config freshness checks          |

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

### Windows

Native Windows: download `crit-windows-amd64.exe` (or `crit-windows-arm64.exe`) from [Releases](https://github.com/tomasz-tomczyk/crit/releases), rename to `crit.exe`, and place it on your `PATH`.

WSL: install the Linux binary as you would on Linux (`go install`, `nix run`, or download `crit-linux-amd64` from Releases). Crit detects WSL and opens URLs in your Windows host browser via `wslview` / `powershell.exe` / `cmd.exe`.

### Docker (sandboxed agents)

For running crit alongside an AI agent inside a container, with the review UI reachable from your host browser, see [`integrations/docker/`](integrations/docker/). Includes a working `Dockerfile` + `entrypoint.sh` that bridges crit's loopback-bound server via `socat` so `docker -p` forwarding works without changing crit's threat model.

## Acknowledgements

Crit embeds the following open-source libraries:

- [markdown-it](https://github.com/markdown-it/markdown-it): Markdown parser
- [highlight.js](https://github.com/highlightjs/highlight.js): Syntax highlighting
- [Mermaid](https://github.com/mermaid-js/mermaid): Diagram rendering
