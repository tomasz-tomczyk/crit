package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// serverConfig holds the resolved configuration for running the server.
// It combines CLI flags, environment variables, and config file settings.
type serverConfig struct {
	port               int
	noOpen             bool
	quiet              bool
	shareURL           string
	authToken          string
	outputDir          string
	author             string
	baseBranch         string // --base-branch override for diff base
	ignorePatterns     []string
	files              []string // explicit file arguments (empty = git mode)
	noIntegrationCheck bool
	noUpdateCheck      bool
	agentCmd           string
	planDir            string // managed storage directory for plan mode
	planName           string // display name for plan content
	reviewPath         string // centralized review file path (~/.crit/reviews/<key>.json)
	vcsOverride        string // "git", "sl"/"sapling", or "" for auto-detect
	cfg                Config // full resolved config for the settings panel

	// focus is populated by resolveFocus when --pr or --range is set;
	// nil means "default" (working-tree, derived inside the session).
	focus *Focus

	// remoteFiles enables API-based file content reads (gh api repos/.../contents/)
	// when in PR/range focus, bypassing the local-fetch + git show path. Diff and
	// changed-file lists still use local git.
	remoteFiles bool
}

// serverFlagSet holds the parsed flag values before config resolution.
type serverFlagSet struct {
	port        int
	noOpen      bool
	showVersion bool
	shareURL    string
	outputDir   string
	quiet       bool
	noIgnore    bool
	baseBranch  string
	vcsOverride string
	planDir     string
	planName    string
	fileArgs    []string

	// PR-scoped / commit-range review (issue #300).
	prSpec    string // --pr <num|url>
	rangeSpec string // --range <baseSHA>..<headSHA>
	scopeSpec string // --scope layer | full-stack

	// remoteFiles is the parsed --remote flag. When true, file content reads
	// in PR/range mode go through `gh api` instead of local git.
	remoteFiles bool
}

func parseServerFlags(args []string) serverFlagSet {
	fs := flag.NewFlagSet("crit", flag.ExitOnError)
	port := fs.Int("port", 0, "Port to listen on (default: random available port)")
	fs.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	shareURL := fs.String("share-url", "", "Base URL of hosted Crit service for sharing reviews (overrides CRIT_SHARE_URL env var)")
	outputDir := fs.String("output", "", "Output directory for review file (default: repo root or file directory)")
	fs.StringVar(outputDir, "o", "", "Output directory for review file (shorthand)")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status output (shorthand)")
	noIgnore := fs.Bool("no-ignore", false, "Disable all ignore patterns from config files")
	baseBranch := fs.String("base-branch", "", "Base branch to diff against (overrides auto-detection)")
	vcsFlag := fs.String("vcs", "", "VCS backend to use: git, sl/sapling (default: auto-detect)")
	planDir := fs.String("plan-dir", "", "")
	planName := fs.String("name", "", "")
	prSpec := fs.String("pr", "", "Review a specific PR by number or URL (e.g. 295 or https://github.com/o/r/pull/295)")
	rangeSpec := fs.String("range", "", "Review a commit range, base..head (e.g. abc1234..def5678)")
	scopeSpec := fs.String("scope", "", "Diff scope when reviewing a PR: layer (default) or full-stack")
	remoteFiles := fs.Bool("remote", false, "Read PR file content via GitHub API instead of local git (avoids `git fetch`; requires gh)")
	fs.Usage = func() {
		printHelp()
	}
	fs.Parse(args)

	return serverFlagSet{
		port:        *port,
		noOpen:      *noOpen,
		showVersion: *showVersion,
		shareURL:    *shareURL,
		outputDir:   *outputDir,
		quiet:       *quiet,
		noIgnore:    *noIgnore,
		baseBranch:  *baseBranch,
		vcsOverride: *vcsFlag,
		planDir:     *planDir,
		planName:    *planName,
		fileArgs:    fs.Args(),
		prSpec:      *prSpec,
		rangeSpec:   *rangeSpec,
		scopeSpec:   *scopeSpec,
		remoteFiles: *remoteFiles,
	}
}

func resolvePort(flagPort, cfgPort int) int {
	if flagPort != 0 {
		return flagPort
	}
	if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			return p
		}
	}
	return cfgPort
}

func applyConfigDefaults(sf *serverFlagSet, cfg Config) {
	sf.port = resolvePort(sf.port, cfg.Port)
	if !sf.noOpen && cfg.NoOpen {
		sf.noOpen = true
	}
	sf.shareURL = resolveShareURL(sf.shareURL, cfg, "")
	if !sf.quiet && cfg.Quiet {
		sf.quiet = true
	}
	if sf.outputDir == "" && cfg.Output != "" {
		sf.outputDir = cfg.Output
	}
	if sf.baseBranch == "" && cfg.BaseBranch != "" {
		sf.baseBranch = cfg.BaseBranch
	}
	if sf.baseBranch != "" {
		setDefaultBranchOverride(sf.baseBranch)
	}
}

// resolveServerConfig parses flags, loads config files, and resolves the
// final server configuration from all sources (CLI > env > config > defaults).
// Returns nil when the command should exit early (e.g. --version).
//
//nolint:unparam // error return is future-proofing for config validation
func resolveServerConfig(args []string) (*serverConfig, error) {
	sf := parseServerFlags(args)

	if sf.showVersion {
		printVersion()
		return nil, nil
	}

	configDir := ""
	vcs := DetectVCS(sf.vcsOverride)
	repoRoot := ""
	if vcs != nil {
		configDir, _ = vcs.RepoRoot()
		repoRoot = configDir
	}
	if configDir == "" {
		configDir = mustGetwd()
	}
	cfg := LoadConfig(configDir)

	applyConfigDefaults(&sf, cfg)

	var ignorePatterns []string
	if !sf.noIgnore {
		ignorePatterns = cfg.IgnorePatterns
	}

	// Resolve --pr / --range / --scope into a Focus. nil = working-tree default.
	focus, err := resolveFocus(sf.prSpec, sf.rangeSpec, sf.scopeSpec, sf.remoteFiles, vcs, repoRoot)
	if err != nil {
		return nil, err
	}

	// --remote only takes effect in PR/range mode. Warn but don't fail.
	if sf.remoteFiles && focus == nil {
		fmt.Fprintln(os.Stderr, "Warning: --remote has no effect without --pr or --range; ignoring")
	}

	return &serverConfig{
		port:               sf.port,
		noOpen:             sf.noOpen,
		quiet:              sf.quiet,
		shareURL:           sf.shareURL,
		authToken:          cfg.AuthToken,
		outputDir:          sf.outputDir,
		author:             cfg.Author,
		baseBranch:         sf.baseBranch,
		ignorePatterns:     ignorePatterns,
		noIntegrationCheck: cfg.NoIntegrationCheck,
		noUpdateCheck:      cfg.NoUpdateCheck,
		agentCmd:           cfg.AgentCmd,
		files:              sf.fileArgs,
		planDir:            sf.planDir,
		planName:           sf.planName,
		vcsOverride:        resolveVCSOverride(sf.vcsOverride, cfg.VCS),
		cfg:                cfg,
		focus:              focus,
		remoteFiles:        sf.remoteFiles,
	}, nil
}

// resolveVCSOverride returns the effective VCS override.
// --vcs flag takes precedence over config "vcs" field.
func resolveVCSOverride(flag, config string) string {
	if flag != "" {
		return flag
	}
	return config
}

// preflightNoChangedFiles runs the git-mode change detection up front so the
// CLI can print a clean message instead of failing inside the daemon (issue
// #438). Returns the user-facing message to print on stderr if there are no
// changes, or "" if the daemon should proceed normally (changes present, not
// a VCS repo, or any other detection error — those are surfaced by the
// daemon's normal init path).
func preflightNoChangedFiles(sc *serverConfig) string {
	vcs := DetectVCS(sc.vcsOverride)
	if vcs == nil {
		return ""
	}
	if sc.baseBranch != "" {
		vcs.SetDefaultBranchOverride(sc.baseBranch)
	}
	root, err := vcs.RepoRoot()
	if err != nil {
		return ""
	}
	_, _, _, _, derr := detectVCSChanges(vcs, root, sc.ignorePatterns)
	if !errors.Is(derr, ErrNoChangedFiles) {
		return ""
	}
	return "No changed files found.\n\n" +
		"  crit              review changed files (needs changes against the base branch)\n" +
		"  crit <file...>    review specific file(s)\n"
}

func createSession(sc *serverConfig) (*Session, error) {
	var session *Session
	var err error
	if len(sc.files) == 0 {
		vcs := DetectVCS(sc.vcsOverride)
		if vcs == nil {
			return nil, fmt.Errorf("not in a version-controlled repository and no files specified")
		}
		if sc.baseBranch != "" {
			vcs.SetDefaultBranchOverride(sc.baseBranch)
		}
		// When --pr/--range is set, the working-tree diff is irrelevant —
		// SetFocus rebuilds the file list from the focus's SHA range. Tolerate
		// an empty working tree so the daemon doesn't crash with
		// "no changed files detected" before the focus is applied (#471).
		session, err = newGitSession(vcs, sc.ignorePatterns, sc.focus == nil)
	} else {
		session, err = NewSessionFromFiles(sc.files, sc.ignorePatterns)
	}
	if err != nil {
		return nil, err
	}
	// Apply --base-branch override to the session's VCS instance. This covers
	// files mode where resolveGitContext creates a fresh VCS that doesn't have
	// the override yet. For Sapling, the instance-level field must be set.
	if sc.baseBranch != "" && session.VCS != nil {
		session.VCS.SetDefaultBranchOverride(sc.baseBranch)
	}
	// Set ReviewFilePath before loadCritJSON so it reads from the centralized
	// review file.
	if sc.reviewPath != "" {
		session.ReviewFilePath = sc.reviewPath
		session.loadCritJSON()
	}
	return session, nil
}

func applySessionOverrides(session *Session, sc *serverConfig) {
	if sc.planDir != "" {
		applyPlanOverrides(session, sc.planDir, sc.planName)
		for _, f := range session.Files {
			f.Comments = []Comment{}
		}
		session.reviewComments = nil
		session.loadCritJSON()
	}
	if sc.outputDir != "" {
		abs, _ := filepath.Abs(sc.outputDir)
		session.OutputDir = abs
	}
	// Apply --pr / --range focus, if requested. SetFocus rebuilds the file
	// list from the SHA range and persists ActiveDiffScope; failure leaves
	// the working-tree state intact and reports via stderr.
	if sc.focus != nil {
		// Set RemoteFiles BEFORE SetFocus so the focus rebuild's file content
		// reads route through the API path instead of local git.
		session.RemoteFiles = sc.remoteFiles
		if err := session.SetFocus(*sc.focus); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to apply focus: %v\n", err)
			return
		}
	}
}

func bindListener(port int) (net.Listener, error) {
	var listener net.Listener
	var err error
	// Retry only makes sense for an explicit port (port != 0): a previous
	// daemon may still be releasing the socket, so brief backoff lets it
	// drain. For an ephemeral port (port == 0) the OS picks a free port —
	// failure means something catastrophic, so break immediately.
	for attempt := 0; attempt < 3; attempt++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
		if port == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, err
}

func serveSessionKey(sc *serverConfig) string {
	cwd, _ := resolvedCWD()
	if sc.planDir != "" {
		return planSessionKey(cwd, sc.planName)
	}
	branch := ""
	if vcs := DetectVCS(sc.vcsOverride); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	return sessionKey(cwd, branch, focusKeyArgs(sc))
}

func checkStaleIntegrations(sc *serverConfig, srv *Server, cwd string) {
	if sc.noIntegrationCheck || os.Getenv("CRIT_NO_INTEGRATION_CHECK") != "" {
		return
	}
	if home, err := os.UserHomeDir(); err == nil {
		stale := checkInstalledIntegrations(cwd, home)
		srv.staleIntegrations = stale
		if len(stale) > 0 {
			go printStaleWarnings(stale)
		}
	}
}

func runServe(args []string) {
	pipe := openReadyPipe()

	sc, err := resolveServerConfig(args)
	if err != nil {
		daemonFatal(pipe, "Error: %v", err)
	}
	if sc == nil {
		return
	}
	sc.quiet = true

	listener, err := bindListener(sc.port)
	if err != nil {
		daemonFatal(pipe, "Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	srv, err := NewServer(nil, frontendFS, sc.shareURL, sc.authToken, sc.author, version, addr.Port, sc.agentCmd)
	if err != nil {
		daemonFatal(pipe, "Error creating server: %v", err)
	}

	// Set config-dependent fields for the settings panel
	srv.cfg = sc.cfg
	cwd, _ := resolvedCWD()
	srv.projectDir = cwd
	if home, err := os.UserHomeDir(); err == nil {
		srv.homeDir = home
	}
	key := serveSessionKey(sc)
	branch := ""
	if vcs := DetectVCS(sc.vcsOverride); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	if sc.outputDir != "" {
		abs, _ := filepath.Abs(sc.outputDir)
		sc.reviewPath = filepath.Join(abs, ".crit")
	} else {
		sc.reviewPath, _ = reviewFilePath(key)
	}
	srv.reviewPath = sc.reviewPath
	srv.cliArgs = sc.files
	if err := writeSessionFile(key, sessionEntry{
		PID:        os.Getpid(),
		Port:       addr.Port,
		CWD:        cwd,
		Args:       sc.files,
		Branch:     branch,
		ReviewPath: sc.reviewPath,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		daemonFatal(pipe, "Error writing session file: %v", err)
	}

	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	// Wire the shutdown ctx into the server so background goroutines (agent
	// runner, etc.) can be cancelled on SIGINT/SIGTERM instead of orphaning
	// subprocesses and racing with WriteFiles.
	srv.SetShutdownCtx(ctx)

	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
			stop()
		}
	}()

	signalReadiness(pipe, addr.Port)

	if !sc.noOpen {
		go openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

	// Prime the open-PR cache in the background. `gh pr list` can take
	// 2-5s on large orgs and the picker waits on it; running this during
	// boot means the first /api/picker call lands on a warm cache instead
	// of paying the network cost while the user watches the page render.
	// Best-effort — failures (no gh, no remote, file mode) are silently
	// dropped; the picker handler still degrades gracefully. Tied to the
	// daemon's shutdown ctx so a Ctrl+C during boot terminates the gh
	// subprocess instead of orphaning it.
	if srv.prList != nil {
		go func() { _, _ = srv.prList.getCtx(ctx) }()
	}

	type sessionResult struct {
		session *Session
		err     error
	}
	ch := make(chan sessionResult, 1)
	// NOTE: On timeout, the createSession goroutine will leak until its git
	// operations finish (no context is threaded into the git calls). This is
	// acceptable because the timeout path sets initErr, which triggers a full
	// server shutdown and process exit shortly after, cleaning up all goroutines.
	go func() {
		s, err := createSession(sc)
		ch <- sessionResult{s, err}
	}()

	var session *Session
	var initErr error
	select {
	case res := <-ch:
		session, initErr = res.session, res.err
	case <-time.After(2 * time.Minute):
		initErr = fmt.Errorf("session initialization timed out after 2 minutes")
	}
	if initErr != nil {
		log.Printf("Error: %v", initErr)
		srv.SetInitErr(initErr)
		// Trigger immediate shutdown instead of waiting for SIGINT.
		// Without this the daemon would sit in 503-land indefinitely
		// after a failed init, burning a port and a process slot.
		stop()
		<-ctx.Done()
		removeSessionFile(key)
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return
	}
	applySessionOverrides(session, sc)
	session.CLIArgs = sc.files

	checkStaleIntegrations(sc, srv, cwd)

	if !sc.noUpdateCheck && os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.CheckForUpdates()
	}
	srv.SetSession(session)

	if session.Mode == "git" {
		go func() {
			if prInfo := detectPRInfo(); prInfo != nil {
				srv.SetPRInfo(prInfo)
			}
		}()
	}

	watchStop := make(chan struct{})
	go session.Watch(watchStop)

	<-ctx.Done()
	close(watchStop)

	removeSessionFile(key)

	// Order matters here:
	//   1. session.Shutdown()    — fires the final SSE "server-shutdown" event
	//                              while clients are still connected.
	//   2. httpServer.Shutdown() — stops accepting new conns and waits for
	//                              in-flight handlers to return. This is what
	//                              gates s.bgWG.Add(1): handleAgentRequest
	//                              calls Add synchronously inside the handler
	//                              before responding 202. Once Shutdown
	//                              returns, no new Add() calls can race with
	//                              the WaitBackground below — sync.WaitGroup
	//                              would otherwise panic on Add-during-Wait.
	//   3. WaitBackground        — drain spawned agent runners so their
	//                              replies land before WriteFiles persists.
	//                              Capped at 30s: a wedged agent loses its
	//                              reply rather than hanging the daemon. The
	//                              agent ctx is parented on the shutdown ctx
	//                              (already Done() above), so subprocesses
	//                              are being killed concurrently — most
	//                              cases drain in milliseconds.
	//   4. WriteFiles            — persist final review state.
	session.Shutdown()

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)

	if !srv.WaitBackground(30 * time.Second) {
		log.Printf("Warning: background goroutines did not drain within 30s; proceeding with shutdown")
	}

	session.WriteFiles()

	if session.ReviewFilePath != "" {
		fmt.Fprintf(os.Stderr, "Review file: %s\n", reviewPathsFor(session.ReviewFilePath).Review)
	}
}
