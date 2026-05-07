package main

import (
	"fmt"
	"os"
	"strings"
)

// commandDispatch maps subcommand names to handler functions.
var commandDispatch = map[string]func([]string){
	"help":      func([]string) { printHelp() },
	"--help":    func([]string) { printHelp() },
	"-h":        func([]string) { printHelp() },
	"--version": func([]string) { printVersion() },
	"-v":        func([]string) { printVersion() },
	"share":     runShare,
	"fetch":     runFetch,
	"unpublish": runUnpublish,
	"install":   runInstall,
	"config":    runConfig,
	"check":     func([]string) { runCheck() },
	"pr":        runPR,
	"pull":      runPull,
	"push":      runPush,
	"comment":   runComment,
	"review":    runReview,
	"plan":      runPlan,
	"plan-hook": func([]string) { runPlanHook() },
	"auth":      runAuth,
	"stop":      runStop,
	"status":    runStatus,
	"cleanup":   runCleanup,
	"_serve":    runServe,
}

func printHelp() {
	fmt.Fprintf(os.Stderr, `crit — inline code review for AI agent workflows

Usage:
  crit                                       Auto-detect changed files via git
  crit <file|dir> [...]                      Review specific files or directories
  crit --pr <num|url>                        Review a GitHub pull request (range mode)
  crit pr <num|url>                          Review a GitHub pull request (alias for --pr)
  crit --range <baseSHA>..<headSHA>          Review a commit range (range mode)
  crit stop [files...]                       Stop the daemon for current directory (and args)
  crit stop --all                            Stop all daemons for current directory
  crit comment <path>:<line[-end]> <body>    Add a review comment
  crit comment --reply-to <id> [--resolve] [--author <name>] <body>  Reply to a comment
  crit comment --json [--author <name>] [--output <dir>]    Read comments from stdin as JSON
  crit comment --clear                       Remove all comments from the review file
  crit share <file> [file...]                Share files to crit-web and print the URL
  crit fetch [--output <dir>]               Fetch comments from crit-web into the review file
  crit unpublish                             Remove a shared review from crit-web
  crit pull [--output <dir>] [pr-number]     Fetch GitHub PR comments into the review file
  crit push [--dry-run] [--event <type>] [-m <msg>] [-o <dir>] [pr-number]  Post review comments to a GitHub PR
  crit plan --name <slug> <file>             Review a plan file (manages versioned copies)
  crit plan --name <slug>                    Read plan from stdin
  crit auth login                            Log in to crit-web via browser
  crit auth logout                           Log out and revoke token
  crit auth whoami                           Show current user info
  crit install <agent>                       Install integration files for an AI coding tool
  crit status [--json]                        Print session info (review file, daemon, comments)
  crit cleanup [--days N] [--force]           Delete stale review files (default: 7 days)
  crit check                                 Check if installed integrations are up to date
  crit config [--generate]                    Show resolved configuration
  crit help                                  Show this help message

  Agents:
    claude-code, cursor, opencode, windsurf, github-copilot, cline, all

Options:
  -p, --port <port>           Port to listen on (default: random)
  -o, --output <dir>          Output directory for review file
      --no-open               Don't auto-open browser
      --no-ignore             Disable all file ignore patterns
  -q, --quiet                 Suppress status output
      --share-url <url>       Share service URL (e.g. https://crit.md or self-hosted)
      --base-branch <branch>  Base branch to diff against (overrides auto-detection)
      --scope <mode>          Diff scope when reviewing a PR: layer (default) or full-stack
      --remote                Read PR file content via GitHub API instead of local git (requires gh)
      --qr                    Print QR code of share URL (with crit share)
  -v, --version               Print version

Environment:
  CRIT_SHARE_URL              Override the share service URL
  CRIT_PORT                   Override the default port
  CRIT_NO_UPDATE_CHECK        Disable update check on startup
  CRIT_AUTH_TOKEN              Override the auth token (skip login)
  CRIT_NO_INTEGRATION_CHECK   Disable integration staleness check

Configuration:
  Global config:   ~/.crit.config.json
  Project config:  .crit.config.json (in repo root)
  agent_cmd        Shell command to send comments to an AI agent (e.g. "claude -p")
  Run 'crit config' to see resolved configuration.

Learn more: https://crit.md
`)
}

func printConfigHelp() {
	fmt.Fprintf(os.Stderr, `crit config — show resolved configuration

Prints the merged configuration from global and project config files as JSON.
CLI flags and environment variables are not reflected in this output.

Config files:
  ~/.crit.config.json          Global config (applies to all projects)
  .crit.config.json            Project config (in repo root)

Precedence (highest to lowest):
  1. CLI flags / env vars
  2. Project config
  3. Global config
  4. Built-in defaults

Available keys:
  port              int       Port to listen on (default: random)
  no_open           bool      Don't auto-open browser (default: false)
  share_url         string    Share service URL
  quiet             bool      Suppress status output (default: false)
  output            string    Output directory for review file
  author            string    Your name for comments (default: git config user.name)
  base_branch       string    Base branch to diff against (overrides auto-detection)
  ignore_patterns        []string  Gitignore-style patterns to exclude files from review
  no_integration_check   bool      Skip integration staleness check (default: false)
  agent_cmd              string    Shell command to send comments to an AI agent (e.g. "claude -p")
  auth_token             string    Authentication token for crit-web share service

Note: agent_cmd and auth_token are global-only (~/.crit.config.json).
Project-level .crit.config.json cannot override them for security reasons.

Ignore pattern syntax:
  *.lock            Match files by extension (anywhere in tree)
  vendor/           Match all files under a directory
  package-lock.json Match exact filename (anywhere in tree)
  generated/*.pb.go Match with path prefix (filepath.Match syntax)

Example config:
  {
    "port": 3456,
    "share_url": "https://crit.md",
    "ignore_patterns": ["*.lock", "*.min.js", "vendor/", "generated/"]
  }
`)
}

func printVersion() {
	line := "crit " + version
	var details []string
	if date != "unknown" {
		details = append(details, date)
	}
	if commit != "unknown" {
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		details = append(details, short)
	}
	if len(details) > 0 {
		line += " (" + strings.Join(details, ", ") + ")"
	}
	fmt.Println(line)
	fmt.Println("Inline code review for AI agent workflows")
}
