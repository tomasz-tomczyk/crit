package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
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
	if len(os.Args) < 2 {
		runServer(os.Args[1:])
		return
	}

	switch os.Args[1] {
	case "help", "--help", "-h":
		printHelp()
	case "go":
		runGo(os.Args[2:])
	case "listen":
		runListen(os.Args[2:])
	case "share":
		runShare(os.Args[2:])
	case "unpublish":
		runUnpublish(os.Args[2:])
	case "install":
		runInstall(os.Args[2:])
	case "config":
		runConfig(os.Args[2:])
	case "pull":
		runPull(os.Args[2:])
	case "push":
		runPush(os.Args[2:])
	case "comment":
		runComment(os.Args[2:])
	default:
		runServer(os.Args[1:])
	}
}

func runGo(args []string) {
	port := requirePort(args, "crit go <port>")
	resp, err := http.Post("http://localhost:"+port+"/api/round-complete", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit on port %s: %v\n", port, err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("Round complete — crit will reload.")
		newStatus(os.Stdout).ListenHint(port)
	} else {
		fmt.Fprintf(os.Stderr, "Unexpected status: %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

func runListen(args []string) {
	port := requirePort(args, "crit listen <port>")
	client := &http.Client{Timeout: 24 * time.Hour}
	resp, err := client.Get("http://localhost:" + port + "/api/wait-for-event")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit on port %s: %v\n", port, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusGatewayTimeout {
		fmt.Fprintln(os.Stderr, "Timeout waiting for event")
		os.Exit(1)
	}
	_, err = io.Copy(os.Stdout, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		os.Exit(1)
	}
}

func runShare(args []string) {
	shareOutputDir := ""
	shareSvcURL := ""
	showQR := false
	var shareArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			shareOutputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --share-url requires a value\n")
				os.Exit(1)
			}
			i++
			shareSvcURL = args[i]
		case arg == "--qr":
			showQR = true
		default:
			shareArgs = append(shareArgs, arg)
		}
	}

	if len(shareArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: crit share [--output <dir>] [--share-url <url>] [--qr] <file> [file...]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Shares files to crit-web and prints the review URL.")
		fmt.Fprintln(os.Stderr, "Comments from .crit.json are included automatically.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  crit share plan.md")
		fmt.Fprintln(os.Stderr, "  crit share plan.md src/main.go")
		fmt.Fprintln(os.Stderr, "  crit share --qr plan.md")
		os.Exit(1)
	}

	shareSvcURL = resolveShareURL(shareSvcURL)

	var files []shareFile
	for _, path := range shareArgs {
		content, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			os.Exit(1)
		}
		relPath := path
		if filepath.IsAbs(path) {
			if wd, err := os.Getwd(); err == nil {
				if rel, err := filepath.Rel(wd, path); err == nil {
					relPath = rel
				}
			}
		}
		files = append(files, shareFile{Path: relPath, Content: string(content)})
	}

	critDir, err := resolveCritDir(shareOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Idempotent: if already shared (same file set), print the existing URL.
	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}
	existingURL, _ := loadExistingShareState(critDir, sharePaths)
	if existingURL != "" {
		fmt.Println(existingURL)
		if showQR {
			fmt.Println()
			qrterminal.GenerateWithConfig(existingURL, qrterminal.Config{
				Level:      qrterminal.L,
				Writer:     os.Stdout,
				HalfBlocks: true,
				QuietZone:  1,
			})
		}
		return
	}

	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.Path
	}
	comments, reviewRound := loadCommentsForShare(critDir, filePaths)

	url, deleteToken, err := shareFilesToWeb(files, comments, shareSvcURL, reviewRound)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := persistShareState(critDir, url, deleteToken, shareScope(filePaths)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state to .crit.json: %v\n", err)
	}

	fmt.Println(url)
	if showQR {
		fmt.Println()
		qrterminal.GenerateWithConfig(url, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     os.Stdout,
			HalfBlocks: true,
			QuietZone:  1,
		})
	}
}

func runUnpublish(args []string) {
	unpubOutputDir := ""
	unpubSvcURL := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			unpubOutputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --share-url requires a value\n")
				os.Exit(1)
			}
			i++
			unpubSvcURL = args[i]
		default:
			fmt.Fprintf(os.Stderr, "Usage: crit unpublish [--output <dir>] [--share-url <url>]\n")
			os.Exit(1)
		}
	}

	unpubSvcURL = resolveShareURL(unpubSvcURL)

	critDir, err := resolveCritDir(unpubOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	critPath := filepath.Join(critDir, ".crit.json")
	data, err := os.ReadFile(critPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: no .crit.json found. Nothing to unpublish.")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}
	if cj.DeleteToken == "" {
		fmt.Fprintln(os.Stderr, "No shared review found in .crit.json — nothing to unpublish.")
		return
	}

	if err := unpublishFromWeb(unpubSvcURL, cj.DeleteToken); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := clearShareState(critDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clear share state from .crit.json: %v\n", err)
	}

	fmt.Println("Review unpublished.")
}

func runInstall(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit install <agent>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Available agents:")
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		fmt.Fprintln(os.Stderr, "  all")
		os.Exit(1)
	}

	force := false
	for _, arg := range args[1:] {
		if arg == "--force" || arg == "-f" {
			force = true
		}
	}

	target := args[0]
	if target == "all" {
		for _, name := range availableIntegrations() {
			installIntegration(name, force)
		}
	} else {
		installIntegration(target, force)
	}
}

func runConfig(args []string) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			printConfigHelp()
			return
		}
		if arg == "--generate" || arg == "-g" {
			fmt.Print(defaultConfig().String())
			return
		}
	}
	configDir := ""
	if IsGitRepo() {
		configDir, _ = RepoRoot()
	}
	if configDir == "" {
		configDir, _ = os.Getwd()
	}
	cfg := LoadConfig(configDir)
	fmt.Print(cfg.String())
}

func runPull(args []string) {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	pullOutputDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			pullOutputDir = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ghComments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load existing .crit.json or create new
	critDir, err := resolveCritDir(pullOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var cj CritJSON
	if data, err := os.ReadFile(filepath.Join(critDir, ".crit.json")); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing .crit.json is invalid, starting fresh: %v\n", err)
		}
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
		cj.Branch = CurrentBranch()
		cj.BaseRef, _ = MergeBase(DefaultBranch())
		cj.ReviewRound = 1
	}

	added := mergeGHComments(&cj, ghComments)

	if added == 0 {
		fmt.Printf("No new inline comments found on PR #%d\n", prNumber)
		return
	}

	if err := writeCritJSON(cj, pullOutputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Pulled %d comments from PR #%d into .crit.json\n", added, prNumber)
	fmt.Println("Run 'crit' to view them in the browser.")
}

func runPush(args []string) {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	dryRun := false
	message := ""
	pushOutputDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dry-run" {
			dryRun = true
			continue
		}
		if arg == "--message" || arg == "-m" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --message requires a value\n")
				os.Exit(1)
			}
			i++
			message = args[i]
			continue
		}
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --output requires a value\n")
				os.Exit(1)
			}
			i++
			pushOutputDir = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit push [--dry-run] [--message <msg>] [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Read .crit.json
	critDir, err := resolveCritDir(pushOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(filepath.Join(critDir, ".crit.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no .crit.json found. Run a crit review first.\n")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}

	ghComments := critJSONToGHComments(cj)
	if len(ghComments) == 0 {
		fmt.Println("No unresolved comments to push.")
		return
	}

	if dryRun {
		fmt.Printf("Would post %d comments to PR #%d:\n\n", len(ghComments), prNumber)
		for _, c := range ghComments {
			path := c["path"].(string)
			line := c["line"].(int)
			body := c["body"].(string)
			if sl, ok := c["start_line"]; ok {
				fmt.Printf("  %s:%d-%d\n", path, sl.(int), line)
			} else {
				fmt.Printf("  %s:%d\n", path, line)
			}
			fmt.Printf("    %s\n\n", body)
		}
		return
	}

	fmt.Printf("Pushing %d comments to PR #%d...\n", len(ghComments), prNumber)
	if err := createGHReview(prNumber, ghComments, message); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Posted %d review comments to PR #%d\n", len(ghComments), prNumber)
}

func runComment(args []string) {
	commentOutputDir := ""
	commentAuthor := ""
	var commentArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			commentOutputDir = args[i]
		} else if arg == "--author" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --author requires a value\n")
				os.Exit(1)
			}
			i++
			commentAuthor = args[i]
		} else {
			commentArgs = append(commentArgs, arg)
		}
	}

	// Handle --clear flag
	if len(commentArgs) >= 1 && commentArgs[0] == "--clear" {
		if err := clearCritJSON(commentOutputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cleared .crit.json")
		return
	}

	if len(commentArgs) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: crit comment [--output <dir>] [--author <name>] <path>:<line[-end]> <body>")
		fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] --clear")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' main.go:42 'Fix this bug'")
		fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' src/auth.go:10-25 'This block needs refactoring'")
		fmt.Fprintln(os.Stderr, "  crit comment --output /tmp/reviews main.go:42 'Fix this bug'")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Tips:")
		fmt.Fprintln(os.Stderr, "  Use --author to identify who left the comment (recommended for AI agents)")
		fmt.Fprintln(os.Stderr, "  Use single quotes for the body to avoid shell interpretation of backticks")
		os.Exit(1)
	}

	// Parse <path>:<line[-end]>
	loc := commentArgs[0]
	colonIdx := strings.LastIndex(loc, ":")
	if colonIdx < 0 {
		fmt.Fprintf(os.Stderr, "Error: invalid location %q — expected <path>:<line[-end]>\n", loc)
		os.Exit(1)
	}
	filePath := loc[:colonIdx]
	lineSpec := loc[colonIdx+1:]

	var startLine, endLine int
	if dashIdx := strings.Index(lineSpec, "-"); dashIdx >= 0 {
		s, err := strconv.Atoi(lineSpec[:dashIdx])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid start line in %q\n", loc)
			os.Exit(1)
		}
		e, err := strconv.Atoi(lineSpec[dashIdx+1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid end line in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = s, e
	} else {
		n, err := strconv.Atoi(lineSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid line number in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = n, n
	}

	// Body is all remaining args joined
	body := strings.Join(commentArgs[1:], " ")

	// Resolve author: --author flag > config > git user.name
	if commentAuthor == "" {
		commentCfgDir, _ := os.Getwd()
		if IsGitRepo() {
			commentCfgDir, _ = RepoRoot()
		}
		commentCfg := LoadConfig(commentCfgDir)
		commentAuthor = commentCfg.Author
	}

	if err := addCommentToCritJSON(filePath, startLine, endLine, body, commentAuthor, commentOutputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
}

// serverConfig holds the resolved configuration for running the server.
// It combines CLI flags, environment variables, and config file settings.
type serverConfig struct {
	port           int
	noOpen         bool
	quiet          bool
	shareURL       string
	outputDir      string
	author         string
	ignorePatterns []string
	files          []string // explicit file arguments (empty = git mode)
}

// resolveServerConfig parses flags, loads config files, and resolves the
// final server configuration from all sources (CLI > env > config > defaults).
// Returns nil when the command should exit early (e.g. --version).
func resolveServerConfig(args []string) (*serverConfig, error) {
	fs := flag.NewFlagSet("crit", flag.ExitOnError)
	port := fs.Int("port", 0, "Port to listen on (default: random available port)")
	fs.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	shareURL := fs.String("share-url", "", "Base URL of hosted Crit service for sharing reviews (overrides CRIT_SHARE_URL env var)")
	outputDir := fs.String("output", "", "Output directory for .crit.json (default: repo root or file directory)")
	fs.StringVar(outputDir, "o", "", "Output directory for .crit.json (shorthand)")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status output (shorthand)")
	noIgnore := fs.Bool("no-ignore", false, "Disable all ignore patterns from config files")
	fs.Usage = func() {
		printHelp()
	}
	fs.Parse(args)

	if *showVersion {
		printVersion()
		return nil, nil
	}

	// Load configuration
	configDir := ""
	if IsGitRepo() {
		configDir, _ = RepoRoot()
	}
	if configDir == "" {
		configDir, _ = os.Getwd()
	}
	cfg := LoadConfig(configDir)

	// CRIT_PORT env var (precedence: CLI flag > env var > config > default)
	if *port == 0 {
		if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
			if p, err := strconv.Atoi(envPort); err == nil {
				*port = p
			}
		}
	}

	// Apply config defaults (CLI flags and env vars override)
	if *port == 0 && cfg.Port != 0 {
		*port = cfg.Port
	}
	if !*noOpen && cfg.NoOpen {
		*noOpen = true
	}
	// Share URL precedence: CLI flag > env var > config > runtime default
	if *shareURL == "" {
		if envShare, ok := os.LookupEnv("CRIT_SHARE_URL"); ok {
			*shareURL = envShare
		} else if cfg.ShareURL != "" {
			*shareURL = cfg.ShareURL
		}
	}
	if !*quiet && cfg.Quiet {
		*quiet = true
	}
	if *outputDir == "" && cfg.Output != "" {
		*outputDir = cfg.Output
	}

	var ignorePatterns []string
	if !*noIgnore {
		ignorePatterns = cfg.IgnorePatterns
	}

	return &serverConfig{
		port:           *port,
		noOpen:         *noOpen,
		quiet:          *quiet,
		shareURL:       *shareURL,
		outputDir:      *outputDir,
		author:         cfg.Author,
		ignorePatterns: ignorePatterns,
		files:          fs.Args(),
	}, nil
}

func runServer(args []string) {
	sc, err := resolveServerConfig(args)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	if sc == nil {
		return // --version or similar early exit
	}

	var session *Session

	if len(sc.files) == 0 {
		// No-args: git mode — auto-detect changed files
		if !IsGitRepo() {
			fmt.Fprintln(os.Stderr, "Error: not in a git repository and no files specified")
			fmt.Fprintln(os.Stderr, "")
			printHelp()
			os.Exit(1)
		}
		session, err = NewSessionFromGit(sc.ignorePatterns)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
	} else {
		// Explicit files
		session, err = NewSessionFromFiles(sc.files, sc.ignorePatterns)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
	}

	if sc.outputDir != "" {
		abs, err := filepath.Abs(sc.outputDir)
		if err != nil {
			log.Fatalf("Error resolving output directory: %v", err)
		}
		session.OutputDir = abs
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sc.port))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	srv, err := NewServer(session, frontendFS, sc.shareURL, sc.author, version, addr.Port)
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

	var statusWriter io.Writer = os.Stdout
	if sc.quiet {
		statusWriter = io.Discard
	}
	status := newStatus(statusWriter)
	srv.status = status
	session.status = status

	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	status.Listening(url)
	status.ListenHint(fmt.Sprintf("%d", addr.Port))

	if !sc.noOpen {
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

// requirePort resolves the port from CLI args, then config, or exits with an error.
func requirePort(args []string, usage string) string {
	port := ""
	if len(args) > 0 {
		port = args[0]
	}
	if port == "" {
		port = resolvePort()
	}
	if port == "" {
		fmt.Fprintf(os.Stderr, "Error: port is required. Usage: %s\n", usage)
		os.Exit(1)
	}
	if _, err := strconv.Atoi(port); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid port %q\n", port)
		os.Exit(1)
	}
	return port
}

// resolvePort returns the configured port as a string, or empty if not configured.
func resolvePort() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	cfg := LoadConfig(dir)
	if cfg.Port != 0 {
		return fmt.Sprintf("%d", cfg.Port)
	}
	return ""
}

func printHelp() {
	fmt.Fprintf(os.Stderr, `crit — inline code review for AI agent workflows

Usage:
  crit                                       Auto-detect changed files via git
  crit <file|dir> [...]                      Review specific files or directories
  crit go <port>                             Signal round-complete to a running crit instance
  crit listen <port>                         Wait for review to finish on a running crit instance
  crit comment <path>:<line[-end]> <body>    Add a review comment to .crit.json
  crit comment --clear                       Remove all comments from .crit.json
  crit share <file> [file...]                Share files to crit-web and print the URL
  crit unpublish                             Remove a shared review from crit-web
  crit pull [--output <dir>] [pr-number]     Fetch GitHub PR comments to .crit.json
  crit push [--dry-run] [--message <msg>] [--output <dir>] [pr-number]  Post .crit.json comments to a GitHub PR
  crit install <agent>                       Install integration files for an AI coding tool
  crit config [--generate]                    Show resolved configuration
  crit help                                  Show this help message

  Agents:
    claude-code, cursor, opencode, windsurf, github-copilot, cline, all

Options:
  -p, --port <port>           Port to listen on (default: random)
  -o, --output <dir>          Output directory for .crit.json
      --no-open               Don't auto-open browser
      --no-ignore             Disable all file ignore patterns
  -q, --quiet                 Suppress status output
      --share-url <url>       Share service URL (e.g. https://crit.live or self-hosted)
      --qr                    Print QR code of share URL (with crit share)
  -v, --version               Print version

Environment:
  CRIT_SHARE_URL              Override the share service URL
  CRIT_PORT                   Override the default port
  CRIT_NO_UPDATE_CHECK        Disable update check on startup

Configuration:
  Global config:   ~/.crit.config.json
  Project config:  .crit.config.json (in repo root)
  Run 'crit config' to see resolved configuration.

Learn more: https://crit.live
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
  output            string    Output directory for .crit.json
  author            string    Your name for comments (default: git config user.name)
  ignore_patterns   []string  Gitignore-style patterns to exclude files from review

Ignore pattern syntax:
  *.lock            Match files by extension (anywhere in tree)
  vendor/           Match all files under a directory
  package-lock.json Match exact filename (anywhere in tree)
  generated/*.pb.go Match with path prefix (filepath.Match syntax)

Example config:
  {
    "port": 3456,
    "share_url": "https://crit.live",
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

type integration struct {
	source string // path inside integrations/ embed
	dest   string // destination relative to cwd
	hint   string // usage hint printed after install
}

var integrationMap = map[string][]integration{
	"claude-code": {
		{source: "integrations/claude-code/commands/crit.md", dest: ".claude/commands/crit.md", hint: "Run /crit in Claude Code to start a review loop"},
	},
	"cursor": {
		{source: "integrations/cursor/commands/crit.md", dest: ".cursor/commands/crit.md", hint: "Run /crit in Cursor to start a review loop"},
	},
	"opencode": {
		{source: "integrations/opencode/crit.md", dest: ".opencode/commands/crit.md", hint: "Run /crit in OpenCode to start a review loop"},
		{source: "integrations/opencode/SKILL.md", dest: ".opencode/skills/crit/SKILL.md", hint: "The crit skill is available to OpenCode agents when needed"},
	},
	"windsurf": {
		{source: "integrations/windsurf/crit.md", dest: ".windsurf/rules/crit.md", hint: "Windsurf will suggest Crit when writing plans"},
	},
	"github-copilot": {
		{source: "integrations/github-copilot/commands/crit.prompt.md", dest: ".github/prompts/crit.prompt.md", hint: "Run /crit in GitHub Copilot to start a review loop"},
	},
	"cline": {
		{source: "integrations/cline/crit.md", dest: ".clinerules/crit.md", hint: "Cline will suggest Crit when writing plans"},
	},
}

func availableIntegrations() []string {
	return []string{"claude-code", "cursor", "opencode", "windsurf", "github-copilot", "cline"}
}

func installIntegration(name string, force bool) {
	files, ok := integrationMap[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown agent: %s\n\nAvailable agents:\n", name)
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		os.Exit(1)
	}

	var hints []string
	for _, f := range files {
		if !force {
			if _, err := os.Stat(f.dest); err == nil {
				fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", f.dest)
				if f.hint != "" {
					hints = append(hints, f.hint)
				}
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
		if f.hint != "" {
			hints = append(hints, f.hint)
		}
	}
	seenHints := make(map[string]bool)
	for _, hint := range hints {
		if seenHints[hint] {
			continue
		}
		seenHints[hint] = true
		fmt.Printf("  %s\n", hint)
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
