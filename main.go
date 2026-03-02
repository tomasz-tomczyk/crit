package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed frontend/*
var frontendFS embed.FS

//go:embed integrations/*
var integrationsFS embed.FS

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// Handle "crit help" subcommand
	if len(os.Args) >= 2 && (os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		printHelp()
		os.Exit(0)
	}

	// Handle "crit go [port]" subcommand — signals round-complete to a running crit server
	if len(os.Args) >= 2 && os.Args[1] == "go" {
		port := "3000" // default
		if len(os.Args) >= 3 {
			port = os.Args[2]
		}
		resp, err := http.Post("http://localhost:"+port+"/api/round-complete", "application/json", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not reach crit on port %s: %v\n", port, err)
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			fmt.Println("Round complete — crit will reload.")
		} else {
			fmt.Fprintf(os.Stderr, "Unexpected status: %d\n", resp.StatusCode)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Handle "crit install [agent]" subcommand
	if len(os.Args) >= 2 && os.Args[1] == "install" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: crit install <agent>")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Available agents:")
			for _, a := range availableIntegrations() {
				fmt.Fprintf(os.Stderr, "  %s\n", a)
			}
			fmt.Fprintln(os.Stderr, "  all")
			os.Exit(1)
		}
		target := os.Args[2]
		if target == "all" {
			for _, name := range availableIntegrations() {
				installIntegration(name)
			}
		} else {
			installIntegration(target)
		}
		os.Exit(0)
	}

	port := flag.Int("port", 0, "Port to listen on (default: random available port)")
	flag.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	noOpen := flag.Bool("no-open", false, "Don't auto-open browser")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	shareURL := flag.String("share-url", "", "Base URL of hosted Crit service for sharing reviews (overrides CRIT_SHARE_URL env var)")
	flag.Usage = func() {
		printHelp()
	}
	flag.Parse()

	if *showVersion {
		printVersion()
		return
	}

	var session *Session
	var err error

	if flag.NArg() == 0 {
		// No-args: git mode — auto-detect changed files
		if !IsGitRepo() {
			fmt.Fprintln(os.Stderr, "Error: not in a git repository and no files specified")
			fmt.Fprintln(os.Stderr, "")
			printHelp()
			os.Exit(1)
		}
		session, err = NewSessionFromGit()
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
	} else {
		// Explicit files
		session, err = NewSessionFromFiles(flag.Args())
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	if *shareURL == "" {
		*shareURL = os.Getenv("CRIT_SHARE_URL")
	}
	if *shareURL == "" {
		*shareURL = "https://crit.live"
	}

	srv, err := NewServer(session, frontendFS, *shareURL, version, addr.Port)
	if err != nil {
		log.Fatalf("Error creating server: %v", err)
	}
	if os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.checkForUpdates()
	}
	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
		// No WriteTimeout — SSE connections need to stay open
	}

	status := newStatus(os.Stdout)
	srv.status = status
	session.status = status

	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	status.Listening(url)

	if !*noOpen {
		go openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watchStop := make(chan struct{})
	go session.Watch(watchStop)

	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	close(watchStop)
	fmt.Println()

	session.Shutdown()
	session.WriteFiles()

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
}

func printHelp() {
	fmt.Fprintf(os.Stderr, `crit — inline code review for AI agent workflows

Usage:
  crit                        Auto-detect changed files via git
  crit <file|dir> [...]       Review specific files or directories
  crit go [port]              Signal round-complete to a running crit instance
  crit install <agent>        Install integration files for an AI coding tool
  crit help                   Show this help message

Agents:
  claude-code, cursor, windsurf, github-copilot, cline, all

Options:
  -p, --port <port>           Port to listen on (default: random)
      --no-open               Don't auto-open browser
      --share-url <url>       Share service URL (default: https://crit.live)
  -v, --version               Print version

Environment:
  CRIT_SHARE_URL              Override the share service URL
  CRIT_NO_UPDATE_CHECK        Disable update check on startup

Learn more: https://crit.live
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

type integration struct {
	source string // path inside integrations/ embed
	dest   string // destination relative to cwd
	hint   string // usage hint printed after install
}

var integrationMap = map[string][]integration{
	"claude-code": {
		{source: "integrations/claude-code/crit.md", dest: ".claude/commands/crit.md", hint: "Run /crit in Claude Code to start a review loop"},
	},
	"cursor": {
		{source: "integrations/cursor/crit-command.md", dest: ".cursor/commands/crit.md", hint: "Run /crit in Cursor to start a review loop"},
	},
	"windsurf": {
		{source: "integrations/windsurf/crit.md", dest: ".windsurf/rules/crit.md", hint: "Windsurf will suggest Crit when writing plans"},
	},
	"github-copilot": {
		{source: "integrations/github-copilot/crit.prompt.md", dest: ".github/prompts/crit.prompt.md", hint: "Run /crit in GitHub Copilot to start a review loop"},
	},
	"cline": {
		{source: "integrations/cline/crit.md", dest: ".clinerules/crit.md", hint: "Cline will suggest Crit when writing plans"},
	},
}

func availableIntegrations() []string {
	return []string{"claude-code", "cursor", "windsurf", "github-copilot", "cline"}
}

func installIntegration(name string) {
	files, ok := integrationMap[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown agent: %s\n\nAvailable agents:\n", name)
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		os.Exit(1)
	}

	force := false
	for _, arg := range os.Args[3:] {
		if arg == "--force" || arg == "-f" {
			force = true
		}
	}

	for _, f := range files {
		if !force {
			if _, err := os.Stat(f.dest); err == nil {
				fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", f.dest)
				continue
			}
		}

		data, err := integrationsFS.ReadFile(f.source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading embedded file %s: %v\n", f.source, err)
			os.Exit(1)
		}

		dir := filepath.Dir(f.dest)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", dir, err)
			os.Exit(1)
		}

		if err := os.WriteFile(f.dest, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", f.dest, err)
			os.Exit(1)
		}

		fmt.Printf("  Installed: %s\n", f.dest)
	}
	if files[0].hint != "" {
		fmt.Printf("  %s\n", files[0].hint)
	}
	fmt.Println()
}

func openBrowser(url string) {
	time.Sleep(200 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Run()
}
