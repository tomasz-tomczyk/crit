package main

import (
	"fmt"
	"os"
	"strings"
)

type commandDescriptor struct {
	name        string
	handler     func([]string)
	help        string
	helpFn      func()
	hidden      bool
	bareHelp    bool
	subcommands []commandDescriptor
}

// commandRegistry is the single source of truth for command dispatch, help,
// ordering, and public visibility.
var commandRegistry = []commandDescriptor{
	{name: "share", handler: runShare, help: `Usage: crit share [options] <file> [file...]
       crit share [options] --preview <file.html>

Share files to crit-web and print the review URL.

Options:
  -o, --output <dir>       Review output directory
      --share-url <url>    Share service URL
      --org <slug>         Organization slug
      --visibility <level> Review visibility
      --preview <file>     Share a local HTML preview
      --qr                 Print a QR code`},
	{name: "fetch", handler: runFetch, help: `Usage: crit fetch [--output <dir>]

Fetch comments from a shared crit-web review.`},
	{name: "unpublish", handler: runUnpublish, help: `Usage: crit unpublish [options] [file...]

Remove a shared review from crit-web.

Options:
  -o, --output <dir>       Review output directory
      --share-url <url>    Share service URL`},
	{name: "install", handler: runInstall, helpFn: printInstallUsage},
	{name: "config", handler: runConfig, bareHelp: true, help: `Usage: crit config [--generate|-g]

Show resolved configuration, or print a starter config with --generate.`},
	{name: "check", handler: func([]string) { runCheck() }, help: `Usage: crit check

Check installed integrations for missing or stale configuration.`},
	{name: "pr", handler: runPR, help: `Usage: crit pr <num|url>

Open a GitHub pull request for review.`},
	{name: "pull", handler: runPull, help: `Usage: crit pull [--output <dir>] [pr-number]

Fetch GitHub pull-request comments into the local review file.`},
	{name: "push", handler: runPush, help: `Usage: crit push [options] [pr-number]

Post local comments as a GitHub pull-request review.

Options:
      --dry-run          Preview without posting
  -e, --event <type>     comment, approve, or request-changes
  -m, --message <text>   Review-level message
  -o, --output <dir>     Review output directory`},
	{name: "comment", handler: runComment, help: `Usage: crit comment [options] <body>
       crit comment [options] <path> <body>
       crit comment [options] <path>:<line[-end]> <body>
       crit comment --reply-to <id> [--resolve] <body>
       crit comment --json [--file <path>]
       crit comment --clear

Add, reply to, bulk import, or clear review comments.

Options:
  -o, --output <dir>   Review output directory
      --author <name>  Comment author
      --plan <name>    Target a stored plan review
      --reply-to <id>  Reply to an existing comment
      --resolve        Resolve the parent after replying
      --path <path>    File path for a reply
      --json           Read bulk comments as JSON
  -f, --file <path>    Read JSON from a file
      --scope <mode>   Override comment focus scope`},
	{name: "comments", handler: runComments, help: `Usage: crit comments [--json] [--all] [review]

List unresolved comments, with review-level comments first.`},
	{name: "review", handler: runReview, help: `Usage: crit review [options] [file|dir...]

Open an inline review for git changes, a commit range, a PR, or files.

Options:
      --pr <num|url>          Review a GitHub pull request
      --range <base>..<head>  Review a commit range
      --base-branch <branch>  Override the diff base
      --no-open               Do not open a browser
  -o, --output <dir>          Review output directory`},
	{name: "live", handler: runLive, help: `Usage: crit live [options] <url>

Review a running web application in live mode.

Options:
  -p, --port <port>        Port to listen on
      --host <host>        Host to listen on
      --public-url <url>   Advertised base URL
      --cookie <value>     Forward a Cookie header
      --cookie-file <path> Read cookies from a file
      --cdp-url <url>      Reuse cookies from Chrome DevTools
      --share-url <url>    Share service URL
      --no-open            Do not open a browser
  -q, --quiet              Suppress status output`},
	{name: "preview", handler: runPreview, help: `Usage: crit preview [options] <file.html>

Review a local HTML file in preview mode.

Options:
  -p, --port <port>       Port to listen on
      --host <host>       Host to listen on
      --public-url <url>  Advertised base URL
      --share-url <url>   Share service URL
      --no-open           Do not open a browser
  -q, --quiet             Suppress status output`},
	{name: "plan", handler: runPlan, help: `Usage: crit plan [--name <slug>] <file>
       echo "content" | crit plan [--name <slug>]

Create or continue a plan-file review. If --name is omitted, crit derives it
from the plan content.`},
	{name: "story", handler: runStory, helpFn: printStoryUsage, bareHelp: true},
	{name: "auth", handler: runAuth, help: `Usage: crit auth <login|logout|whoami>

Manage crit-web authentication.

Commands:
  login     Log in to crit-web
  logout    Log out and revoke the saved token
  whoami    Show the current user`, subcommands: []commandDescriptor{
		{name: "login", help: `Usage: crit auth login [--force]

Log in to crit-web with the device authorization flow.

Options:
      --force  Reauthenticate even when already logged in`},
		{name: "logout", help: `Usage: crit auth logout

Revoke the current token and remove saved credentials.`},
		{name: "whoami", help: `Usage: crit auth whoami

Show the currently authenticated crit-web user.`},
	}},
	{name: "stop", handler: runStop, help: `Usage: crit stop [--all] [file...]

Stop the review daemon for the current session. Specify files to target an
exact file-mode session, or use --all to stop every daemon.`},
	{name: "status", handler: runStatus, help: `Usage: crit status [--json]

Show the review path, daemon status, and comment counts.`},
	{name: "stats", handler: runStats, help: `Usage: crit stats [--json]

Show lifetime review statistics.`},
	{name: "cleanup", handler: runCleanup, help: `Usage: crit cleanup [--days N] [--force]

Delete stale review files. The default age is seven days.`},
	{name: "plan-hook", handler: runPlanHookCommand, help: `Usage: crit plan-hook [--mode claude|codex]

Run the internal plan hook.`, hidden: true},
	{name: "_serve", handler: runServe, help: `Usage: crit _serve [options]

Run the internal foreground review server.`, hidden: true},
}

func dispatchCLI(args []string) (bool, error) {
	return dispatchWithRegistry(args, commandRegistry)
}

func dispatchWithRegistry(args []string, registry []commandDescriptor) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--help", "-h":
		printHelp()
		return true, nil
	case "--version", "-v":
		printVersion()
		return true, nil
	case "help":
		return true, printRequestedHelp(args[1:], registry)
	}

	command, ok := findCommand(registry, args[0])
	if !ok {
		return false, nil
	}
	commandArgs := args[1:]
	if len(commandArgs) > 0 && (isHelpFlag(commandArgs[0]) || command.bareHelp && commandArgs[0] == "help") {
		printCommandHelp(command)
		return true, nil
	}
	if len(command.subcommands) > 0 && len(commandArgs) >= 2 {
		if subcommand, found := findCommand(command.subcommands, commandArgs[0]); found && isHelpFlag(commandArgs[1]) {
			printCommandHelp(subcommand)
			return true, nil
		}
	}
	command.handler(commandArgs)
	return true, nil
}

func printRequestedHelp(args []string, registry []commandDescriptor) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}
	command, ok := findCommand(registry, args[0])
	if !ok {
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	if len(args) > 1 {
		if subcommand, found := findCommand(command.subcommands, args[1]); found {
			if len(args) > 2 {
				return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
			}
			printCommandHelp(subcommand)
			return nil
		}
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	printCommandHelp(command)
	return nil
}

func findCommand(registry []commandDescriptor, name string) (commandDescriptor, bool) {
	for _, command := range registry {
		if command.name == name {
			return command, true
		}
	}
	return commandDescriptor{}, false
}

func isHelpFlag(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func printCommandHelp(command commandDescriptor) {
	if command.helpFn != nil {
		command.helpFn()
		return
	}
	fmt.Fprintln(os.Stderr, command.help)
}

func printHelp() {
	fmt.Fprintf(os.Stderr, `crit — inline code review for AI agent workflows

Getting started:
  crit install <agent>                       Set up crit for your AI coding tool
  crit                                       Review your current changes (auto-detects git)

Commands:
  %s

Review:
  crit                                       Auto-detect changed files via git
  crit <file|dir> [...]                      Review specific files or directories
  crit live <url>                            Review a running web app in live mode
  crit preview <file.html>                   Review a local HTML file in preview mode
  crit --pr <num|url>                        Review a GitHub pull request
  crit --range <base>..<head>                Review a commit range
  crit plan --name <slug> <file>             Review a plan file
  crit story                                 Generate and review a story-mode diff
  crit --session <id>                        Reconnect to an existing review session

Comments:
  crit comment <path>:<line[-end]> <body>    Add a comment (headless, no server needed)
  crit comment --reply-to <id> <body>        Reply to a comment
  crit comment --json                        Bulk add comments from JSON on stdin
  crit comment --clear                       Remove all comments
  crit comments [--json] [--all] [review]    List unresolved comments (review-level first)

Sharing:
  crit share <file> [file...]                Share files to crit-web, print URL
  crit fetch [--output <dir>]                Fetch comments from crit-web
  crit unpublish [file...]                   Remove a shared review from crit-web

GitHub PR sync:
  crit pull [pr-number]                      Fetch PR comments into the review file
  crit push [--dry-run] [pr-number]          Post review comments to a GitHub PR

Setup & management:
  crit install <agent>                       Install integration for an AI coding tool
  crit check                                 Check integrations (staleness + missing)
  crit status [--json]                       Print session info
  crit stats [--json]                        Show lifetime review statistics
  crit stop [--all]                          Stop the daemon
  crit cleanup [--days N] [--force]          Delete stale review files (default: 7 days)
  crit config [--generate]                   Show resolved configuration
  crit auth login|logout|whoami              Manage crit-web authentication

  Agents: %s, all

Options:
  -p, --port <port>           Port to listen on (default: random)
      --host <host>           Listen host (default: 127.0.0.1; e.g. 0.0.0.0 for LAN)
      --public-url <url>      Advertised base URL (e.g. https://machine.ts.net via tailscale serve)
  -o, --output <dir>          Crit data root for reviews (default: ~/.crit)
      --no-open               Don't auto-open browser
      --no-ignore             Disable all file ignore patterns
  -q, --quiet                 Suppress status output
      --share-url <url>       Share service URL (e.g. https://crit.md or self-hosted)
      --base-branch <branch>  Base branch to diff against (overrides auto-detection)
      --scope <mode>          Diff scope for PR review: layer (default) or full-stack
      --session <id>          Reconnect to an existing review session (from stderr or next_command)
      --remote                Read PR files via GitHub API instead of local git
      --qr                    Print QR code of share URL (with crit share)
  -v, --version               Print version

Environment:
  CRIT_SHARE_URL              Override the share service URL
  CRIT_PUBLIC_URL             Override the advertised review URL (listen address unchanged)
  CRIT_PORT                   Override the default port
  CRIT_HOST                   Override the listen host (default 127.0.0.1)
  CRIT_NO_UPDATE_CHECK        Disable update check on startup
  CRIT_AUTH_TOKEN             Override the auth token (skip login)
  CRIT_NO_INTEGRATION_CHECK   Disable staleness check and agent detection on startup

Configuration:
  Global: ~/.crit.config.json   Project: .crit.config.json (in repo root)
  Run 'crit config' to see all keys and resolved values.

Learn more: https://crit.md
`, strings.Join(visibleCommandNames(), ", "), strings.Join(availableIntegrations(), ", "))
}

func visibleCommandNames() []string {
	var names []string
	for _, command := range commandRegistry {
		if !command.hidden {
			names = append(names, command.name)
		}
	}
	return names
}

func runPlanHookCommand(args []string) {
	mode := "claude"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mode":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: --mode requires a value")
				os.Exit(1)
			}
			i++
			mode = args[i]
		case strings.HasPrefix(arg, "--mode="):
			mode = strings.TrimPrefix(arg, "--mode=")
		default:
			fmt.Fprintf(os.Stderr, "Unknown plan-hook flag: %s\n", arg)
			os.Exit(1)
		}
	}

	switch mode {
	case "claude", "":
		runPlanHook()
	case "codex":
		runCodexPlanHook()
	default:
		fmt.Fprintf(os.Stderr, "Unknown plan-hook mode: %s\n", mode)
		os.Exit(1)
	}
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
  host              string    Listen host (default: 127.0.0.1; e.g. 0.0.0.0 for LAN)
  no_open           bool      Don't auto-open browser (default: false)
  share_url         string    Share service URL (global config only)
  proxy_auth        bool      Proxy auth mode (config-only, no flag/env). false (default) —
                              local server contacts crit-web directly. true — browser opens
                              crit-web in a popup, authenticates there (e.g. via SSO), and
                              proxies share/pull/unpublish/re-share through a MessagePort.
                              Use when crit-web is behind an SSO reverse proxy.
  quiet             bool      Suppress status output (default: false)
  output            string    Crit data root for reviews (default: ~/.crit; reviews in <root>/reviews/<key>/)
  author            string    Your name for comments (default: git config user.name)
  base_branch       string    Base branch to diff against (overrides auto-detection)
  vcs                    string    Preferred VCS backend: git, sl, or jj (default: auto-detect)
  ignore_patterns        []string  Gitignore-style patterns to exclude files from review
  auto_viewed_patterns   []string  Patterns whose files are auto-marked viewed once per launch
  no_integration_check   bool      Skip integration staleness check (default: false)
  no_update_check        bool      Disable update check on startup (default: false)
  cleanup_on_approve     bool      Auto-delete review file when approved (default: true)
  notify_on_round_ready  bool      Desktop notification when a round is ready (default: false)
  disable_stats          bool      Disable session stats recording (default: false)
  open_cmd               string    Custom browser/open command
  agent_cmd              string    Shell command to send comments to an AI agent (e.g. "claude -p")
  auth_token             string    Authentication token for crit-web share service
  plan_approve_mode      string    Claude Code mode after plan approval (default: unset)
  close_on_approve_after_ms int    Auto-close the review tab N ms after Approve (default: unset/disabled)

Note: agent_cmd, auth_token, host, open_cmd, share_url, plan_approve_mode, and
close_on_approve_after_ms are global-only (~/.crit.config.json). Project-level
.crit.config.json cannot override them for security reasons.

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
