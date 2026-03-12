# Crit

Your agent writes plans and code. Before any of it lands, review it. Crit opens a browser-based UI where you leave inline comments on any file: plans, code diffs, specs, whatever your agent produced. Click "Finish Review" and a structured prompt goes to your clipboard. Paste it back, the agent iterates, Crit shows you the diff. Repeat until it's right.

```bash
crit              # auto-detect changed files in your repo
crit plan.md      # review specific files
```

Works with Claude Code, Cursor, GitHub Copilot, Aider, Cline, Windsurf - any agent that reads files.

![Crit review UI](images/demo-overview.png)

## Workflow

```bash
# Review changed files in your repo
crit
# → Browser opens with all changed files as diffs
# → File tree shows added/modified/deleted files

# Review a specific file (plan, spec, any markdown)
crit plan.md

# Review multiple files
crit plan.md api-spec.md

# In all cases:
# → Select lines, leave inline comments
# → Click "Finish Review", prompt copied to clipboard
# → Paste into your agent
# → Agent reads .crit.json, addresses comments, runs `crit go <port>`
# → New round starts with a diff of what changed
# → Previous comments show as resolved or still open
# → Repeat until it's right
```

### Output

When you finish a review, Crit generates `.crit.json`, structured comment data that your agent reads and acts on. Add it to your `.gitignore`:

```bash
echo '.crit.json' >> .gitignore
```

## Demo

A 5-minute walkthrough: leaving inline comments on a plan, followed by branch review (`crit` with no args), wchih uses the same UI with git diffs instead of rendered markdown.

[![Crit demo](https://github.com/user-attachments/assets/dec9c069-9a99-4254-9b05-6d8db30820ed)](https://www.youtube.com/watch?v=XRjkRpXuLJc)

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
# Run without installing
nix run github:tomasz-tomczyk/crit -- --help
```

Or add it to a `flake.nix`:

```nix
inputs.crit.url = "github:tomasz-tomczyk/crit";
```

If you want to use the package directly from that input, reference the named package:

```nix
inputs.crit.packages.${pkgs.stdenv.hostPlatform.system}.crit
```

If you prefer a local alias in one module, use a `let` binding:

```nix
let
  crit = inputs.crit.packages.${pkgs.stdenv.hostPlatform.system}.crit;
in {
  environment.systemPackages = with pkgs; [
    crit
  ];
}
```

If you want `pkgs.crit` everywhere, add an overlay:

```nix
# overlays/crit.nix
inputs: final: _prev: {
  crit = inputs.crit.packages.${final.stdenv.hostPlatform.system}.crit;
}
```

```nix
# then include it in your nixpkgs overlays
(import ./overlays/crit.nix inputs)
```

### Download Binary

Grab the latest binary for your platform from [Releases](https://github.com/tomasz-tomczyk/crit/releases).

## Features

### Git review

Run `crit` with no arguments. Crit auto-detects changed files in your repo and opens them as syntax-highlighted git diffs. A file tree on the left shows every file with its status (added, modified, deleted) and comment counts. Toggle between split and unified diff views.

![Crit review for your branch](images/git-mode.png)

### File review

Pass specific files to review them directly: `crit plan.md api-spec.md`. Markdown files render as formatted documents with per-line commenting. Code files show as syntax-highlighted source. Both support the same inline comment workflow and multi-round iteration.

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

### Share for async review

Want a second opinion before handing off to the agent? Enable sharing by setting `CRIT_SHARE_URL=https://crit.live` (or pass `--share-url`), then click the Share button to upload your review and get a public URL anyone can open in a browser, no install needed. Each reviewer's comments are color-coded by author. Unpublish anytime.

### Finish review: prompt copied to clipboard

When you click "Finish Review", Crit collects your comments, formats them into a prompt, and copies it to your clipboard. Paste directly into your agent.

![Agent prompt](images/prompt.png)

### Mermaid diagrams

Architecture diagrams in fenced ` ```mermaid ` blocks render inline. You can comment on the diagram source just like any other block.

![Mermaid diagram](images/mermaid.png)

### Everything else

- **Single binary.** No daemon, no Docker, no dependencies. `brew install` and you're done.
- **Draft autosave.** Close your browser mid-review and pick up exactly where you left off.
- **Vim keybindings.** `j`/`k` to navigate, `c` to comment, `Shift+F` to finish. `?` for the full reference.
- **Concurrent reviews.** Each instance runs on its own port - review multiple plans at once.
- **Syntax highlighting.** Code blocks are highlighted and split per-line, so you can comment on individual lines inside a fence.
- **Live file watching.** The browser reloads automatically when the source file changes.
- **Real-time output.** `.crit.json` is written on every comment change (200ms debounce), so your agent always has the latest review state.
- **Dark/light/system theme.** Three-button pill in the header, persisted to localStorage.
- **Local by default.** Server binds to `127.0.0.1`. Your files stay on your machine unless you explicitly share.

## Agent Integrations

Crit ships with drop-in configuration files for popular AI coding tools. Each one teaches your agent to write a plan, launch `crit` for review, and wait for your feedback before implementing.

The fastest way to set up an integration:

```bash
crit install claude-code   # or: cursor, opencode, windsurf, github-copilot, cline
crit install all           # install all integrations at once
```

This copies the right files to the right places in your project. Safe to re-run - existing files are skipped (use `--force` to overwrite).

Or set up manually:

| Tool               | Setup                                                                                 |
| ------------------ | ------------------------------------------------------------------------------------- |
| **Claude Code**    | Copy `integrations/claude-code/crit.md` to `.claude/commands/crit.md`                 |
| **Cursor**         | Copy `integrations/cursor/crit-command.md` to `.cursor/commands/crit.md`              |
| **OpenCode**       | Copy `integrations/opencode/crit.md` to `.opencode/commands/crit.md`                  |
| **OpenCode**       | Copy `integrations/opencode/SKILL.md` to `.opencode/skills/crit-review/SKILL.md`      |
| **GitHub Copilot** | Copy `integrations/github-copilot/crit.prompt.md` to `.github/prompts/crit.prompt.md` |
| **Windsurf**       | Copy `integrations/windsurf/crit.md` to `.windsurf/rules/crit.md`                     |
| **Aider**          | Append `integrations/aider/CONVENTIONS.md` to your `CONVENTIONS.md`                   |
| **Cline**          | Copy `integrations/cline/crit.md` to `.clinerules/crit.md`                            |

See [`integrations/`](integrations/) for the full files and details.

### `/crit` command

Claude Code, Cursor, OpenCode, and GitHub Copilot support a `/crit` slash command that automates the full review loop:

```
/crit              # Auto-detects the current plan file
/crit my-plan.md   # Review a specific file
```

It launches Crit, waits for your review, reads your comments, revises the plan, and signals Crit for another round. OpenCode also ships with a `crit-review` skill that agents can load on demand. Other tools use rules files that teach the agent to suggest Crit when writing plans.

## Usage

```bash
# Git mode: review all changed files (auto-detected)
crit

# Review specific files
crit plan.md
crit plan.md api-spec.md

# Specify a port
crit -p 3000 plan.md

# Don't auto-open browser
crit --no-open plan.md
```

## Programmatic Comments

AI agents can use `crit comment` to add inline review comments without opening the browser UI or constructing JSON manually:

```bash
crit comment src/auth.go:42 'Missing null check'
crit comment src/handler.go:15-28 'Error handling issue'
crit comment --output /tmp/reviews src/auth.go:42 'comment'  # custom output dir
crit comment --clear   # remove .crit.json
```

Comments are appended to `.crit.json` — created automatically if it doesn't exist. Run `crit install <agent>` to install the integration, which includes a `crit-comment` skill file teaching your agent the syntax.

## GitHub PR Sync

Crit can sync review comments bidirectionally with GitHub PRs. Requires the [GitHub CLI](https://cli.github.com) (`gh`) to be installed and authenticated.

### Pull comments from a PR

```bash
crit pull              # auto-detects PR from current branch
crit pull 42           # explicit PR number
```

### Push comments to a PR

```bash
crit push                          # auto-detects PR from current branch
crit push --dry-run                # preview without posting
crit push --message "Round 2"      # add a top-level review comment
crit push 42                       # explicit PR number
```

## Configuration

Crit supports persistent configuration via JSON files so you don't have to pass the same flags every time.

| File                    | Scope   | Location                          |
| ----------------------- | ------- | --------------------------------- |
| `~/.crit.config.json`   | Global  | Applies to all projects           |
| `.crit.config.json`     | Project | Repo root (from `git rev-parse --show-toplevel`) |

Project config overrides global. CLI flags and env vars override both.

```bash
# Generate a starter config with all available keys
crit config --generate > .crit.config.json

# View resolved config (merged global + project)
crit config

# See all available keys and pattern syntax
crit config --help
```

### Example

```json
{
  "port": 3456,
  "no_open": false,
  "share_url": "https://crit.live",
  "quiet": false,
  "output": "",
  "ignore_patterns": [
    "*.lock",
    "*.min.js",
    "vendor/",
    "generated/*.pb.go"
  ]
}
```

All keys are optional — omit any you don't need.

### Ignore patterns

Patterns from global and project configs are merged (union). Supported syntax:

| Pattern              | Matches                                          |
| -------------------- | ------------------------------------------------ |
| `*.lock`             | Files ending in `.lock` anywhere in tree         |
| `vendor/`            | All files under `vendor/`                        |
| `package-lock.json`  | Exact filename anywhere in tree                  |
| `generated/*.pb.go`  | Path prefix with glob (`filepath.Match` syntax)  |

Use `--no-ignore` to temporarily disable all ignore patterns:

```bash
crit --no-ignore   # review everything, including ignored files
```

## Environment Variables

| Variable               | Description                                                                                |
| ---------------------- | ------------------------------------------------------------------------------------------ |
| `CRIT_SHARE_URL`       | Enable the Share button (e.g. `https://crit.live` or a self-hosted instance) |
| `CRIT_PORT`            | Override the default port (between config file and CLI flag in precedence)                  |
| `CRIT_NO_UPDATE_CHECK` | Set to any value to disable the update check on startup                                    |

## Build from Source

Requires Go 1.26+ (install via [asdf](https://asdf-vm.com/), Homebrew, or [go.dev](https://go.dev/dl/)):

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

### E2E Tests

The `e2e/` directory has a Playwright test suite that runs the full frontend against a real Crit server. Requires Node.js (listed in `mise.toml`).

```bash
cd e2e && npm install && npx playwright install chromium

make e2e                                              # Run full suite
cd e2e && npx playwright test tests/comments.spec.ts  # Run one test file
cd e2e && npx playwright test --headed                # Run with visible browser
make e2e-report                                       # View HTML report
```

## Acknowledgments

Crit embeds the following open-source libraries:

- [markdown-it](https://github.com/markdown-it/markdown-it): Markdown parser
- [highlight.js](https://github.com/highlightjs/highlight.js): Syntax highlighting
- [Mermaid](https://github.com/mermaid-js/mermaid): Diagram rendering
