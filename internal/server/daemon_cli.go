package server

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// PrintHelpFn is wired from cmd/crit for flag-set usage text.
var PrintHelpFn func()

// PrintVersionFn is wired from cmd/crit for --version early exit.
var PrintVersionFn func()

// DaemonCLIConfig holds resolved daemon configuration from CLI flags, env, and config files.
type DaemonCLIConfig struct {
	Port               int
	Host               string
	NoOpen             bool
	Quiet              bool
	ShareURL           string
	ProxyAuth          bool
	AuthToken          string
	OutputDir          string
	Author             string
	BaseBranch         string
	IgnorePatterns     []string
	Files              []string
	NoIntegrationCheck bool
	NoUpdateCheck      bool
	AgentCmd           string
	PlanDir            string
	PlanName           string
	ReviewPath         string
	VCSOverride        string
	Cfg                config.Config
	Focus              *session.Focus
	RemoteFiles        bool
	LiveOrigin         string
	LiveCookie         string
	PreviewFile        string
}

type daemonFlagSet struct {
	port        int
	host        string
	noOpen      bool
	showVersion bool
	shareURL    string
	proxyAuth   bool
	outputDir   string
	quiet       bool
	noIgnore    bool
	baseBranch  string
	vcsOverride string
	planDir     string
	planName    string
	fileArgs    []string
	prSpec      string
	rangeSpec   string
	scopeSpec   string
	remoteFiles bool
	liveOrigin  string
	liveCookie  string
	previewFile string
}

func parseDaemonFlags(args []string) daemonFlagSet {
	fs := flag.NewFlagSet("crit", flag.ExitOnError)
	port := fs.Int("port", 0, "Port to listen on (default: random available port)")
	fs.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	host := fs.String("host", "", "Host to listen on (default: 127.0.0.1; e.g. 0.0.0.0 to expose on LAN — no auth, opt in deliberately)")
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
	vcsFlag := fs.String("vcs", "", "VCS backend to use: git, sl/sapling, jj/jujutsu (default: auto-detect)")
	planDir := fs.String("plan-dir", "", "")
	planName := fs.String("name", "", "")
	prSpec := fs.String("pr", "", "Review a specific PR by number or URL (e.g. 295 or https://github.com/o/r/pull/295)")
	rangeSpec := fs.String("range", "", "Review a commit range, base..head (e.g. abc1234..def5678)")
	scopeSpec := fs.String("scope", "", "Diff scope when reviewing a PR: layer (default) or full-stack")
	remoteFiles := fs.Bool("remote", false, "Read PR file content via GitHub API instead of local git (avoids `git fetch`; requires gh)")
	liveOrigin := fs.String("live-origin", "", "")
	liveCookie := fs.String("live-cookie", "", "")
	previewFile := fs.String("preview-file", "", "")
	fs.Usage = func() {
		if PrintHelpFn != nil {
			PrintHelpFn()
		}
	}
	fs.Parse(args)

	return daemonFlagSet{
		port:        *port,
		host:        *host,
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
		liveOrigin:  *liveOrigin,
		liveCookie:  *liveCookie,
		previewFile: *previewFile,
	}
}

func applyDaemonConfigDefaults(sf *daemonFlagSet, cfg config.Config) {
	sf.port = config.ResolvePort(sf.port, cfg.Port)
	sf.host = config.ResolveHost(sf.host, cfg.Host)
	if !sf.noOpen && cfg.NoOpen {
		sf.noOpen = true
	}
	sf.shareURL = config.ResolveShareURL(sf.shareURL, cfg, "")
	sf.proxyAuth = cfg.ProxyAuth
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
		vcs.SetDefaultBranchOverride(sf.baseBranch)
	}
}

func daemonMustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit: unable to determine current working directory: %v\n", err)
		os.Exit(1)
	}
	return wd
}

func resolveVCSOverride(flagVal, cfgVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return cfgVal
}

// ResolveDaemonCLIConfig parses flags, loads config files, and resolves the
// final daemon configuration from all sources (CLI > env > config > defaults).
// Returns nil when the command should exit early (e.g. --version).
func ResolveDaemonCLIConfig(args []string) (*DaemonCLIConfig, error) {
	sf := parseDaemonFlags(args)

	if sf.showVersion {
		if PrintVersionFn != nil {
			PrintVersionFn()
		}
		return nil, nil
	}

	configDir := ""
	v := vcs.DetectVCS(sf.vcsOverride)
	repoRoot := ""
	if v != nil {
		configDir, _ = v.RepoRoot()
		repoRoot = configDir
	}
	if configDir == "" {
		configDir = daemonMustGetwd()
	}
	cfg := config.LoadConfig(configDir)

	applyDaemonConfigDefaults(&sf, cfg)

	var ignorePatterns []string
	if !sf.noIgnore {
		ignorePatterns = cfg.IgnorePatterns
	}

	f, err := focus.ResolveFocus(sf.prSpec, sf.rangeSpec, sf.scopeSpec, sf.remoteFiles, v, repoRoot)
	if err != nil {
		return nil, err
	}

	remoteFiles := sf.remoteFiles
	if remoteFiles && f == nil {
		fmt.Fprintln(os.Stderr, "Warning: --remote has no effect without --pr or --range; ignoring")
		remoteFiles = false
	}

	return &DaemonCLIConfig{
		Port:               sf.port,
		Host:               sf.host,
		NoOpen:             sf.noOpen,
		Quiet:              sf.quiet,
		ShareURL:           sf.shareURL,
		ProxyAuth:          sf.proxyAuth,
		AuthToken:          cfg.AuthToken,
		OutputDir:          sf.outputDir,
		Author:             cfg.Author,
		BaseBranch:         sf.baseBranch,
		IgnorePatterns:     ignorePatterns,
		NoIntegrationCheck: cfg.NoIntegrationCheck,
		NoUpdateCheck:      cfg.NoUpdateCheck,
		AgentCmd:           cfg.AgentCmd,
		Files:              sf.fileArgs,
		PlanDir:            sf.planDir,
		PlanName:           sf.planName,
		VCSOverride:        resolveVCSOverride(sf.vcsOverride, cfg.VCS),
		Cfg:                cfg,
		Focus:              f,
		RemoteFiles:        remoteFiles,
		LiveOrigin:         sf.liveOrigin,
		LiveCookie:         sf.liveCookie,
		PreviewFile:        sf.previewFile,
	}, nil
}

// PreflightCheck runs pre-spawn checks for default git mode (no files, no
// focus, no plan). Returns a user-facing message to print on stderr if the
// daemon should not be spawned, or "" if everything looks fine.
func PreflightCheck(sc *DaemonCLIConfig) string {
	if sc == nil {
		return ""
	}
	v := vcs.DetectVCS(sc.VCSOverride)
	if v == nil {
		return "Not in a version-controlled repository.\n\n" +
			"  crit              review changed files (run inside a git/sapling/jj repo)\n" +
			"  crit <file...>    review specific file(s)\n"
	}
	if sc.BaseBranch != "" {
		v.SetDefaultBranchOverride(sc.BaseBranch)
	}
	root, err := v.RepoRoot()
	if err != nil {
		return ""
	}
	_, _, _, _, derr := session.DetectVCSChanges(v, root, sc.IgnorePatterns)
	if !errors.Is(derr, session.ErrNoChangedFiles) {
		return ""
	}
	return "No changed files found.\n\n" +
		"  crit              review changed files (needs changes against the base branch)\n" +
		"  crit <file...>    review specific file(s)\n"
}

// FocusKeyArgs returns the args slice used to key the daemon session for a
// PR/range focus. PR-keyed daemons reuse the same review file across head
// changes; range-keyed daemons are unique per (base, head) pair.
//
// DiffScope is NOT part of the key — the picker must let users toggle scopes
// within a single session.
func FocusKeyArgs(sc *DaemonCLIConfig) []string {
	if sc == nil || sc.Focus == nil || sc.Focus.Kind != session.FocusRange {
		if sc == nil {
			return nil
		}
		return sc.Files
	}
	if sc.Focus.PRNumber > 0 {
		return []string{fmt.Sprintf("pr:%d", sc.Focus.PRNumber)}
	}
	return []string{fmt.Sprintf("range:%s..%s", sc.Focus.BaseSHA, sc.Focus.HeadSHA)}
}

// ResolvePortFromEnv mirrors config.ResolvePort for tests that assemble DaemonCLIConfig manually.
func ResolvePortFromEnv(flagPort, cfgPort int) int {
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
