package session

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

type planConfig struct {
	name                        string
	filePath                    string
	stdinExpected               bool
	port                        int
	host                        string
	publicURL                   string
	allowUnauthenticatedNetwork bool
	noOpen                      bool
	quiet                       bool
	shareURL                    string
}

func resolvePlanConfig(args []string) planConfig {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	name := fs.String("name", "", "Plan name/slug for session identification")
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	host := fs.String("host", "", "Host to listen on")
	publicURL := fs.String("public-url", "", "Advertised base URL (overrides CRIT_PUBLIC_URL)")
	allowUnauthNet := fs.Bool(config.AllowUnauthenticatedNetworkFlag, false, "Allow non-loopback listen or public_url without authentication")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	quiet := fs.Bool("quiet", false, "On success, suppress connect/start status, tips, and session summary")
	fs.BoolVar(quiet, "q", false, "On success, suppress status (shorthand)")
	shareURL := fs.String("share-url", "", "Share service URL")
	// Keep in sync with the fs.Bool/fs.BoolVar registrations above.
	args = clicmd.ReorderFlagsFirst(args, map[string]bool{
		config.AllowUnauthenticatedNetworkFlag: true,
		"no-open":                              true,
		"quiet":                                true,
		"q":                                    true,
	})
	fs.Parse(args)

	pc := planConfig{
		name:                        *name,
		port:                        *port,
		host:                        *host,
		publicURL:                   *publicURL,
		allowUnauthenticatedNetwork: *allowUnauthNet,
		noOpen:                      *noOpen,
		quiet:                       *quiet,
		shareURL:                    *shareURL,
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		pc.filePath = remaining[0]
	} else {
		pc.stdinExpected = true
	}

	return pc
}

func readPlanContent(pc planConfig) ([]byte, error) {
	var content []byte
	var err error

	if pc.filePath != "" {
		content, err = os.ReadFile(pc.filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", pc.filePath, err)
			return nil, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
	} else if pc.stdinExpected {
		if !IsStdinPipe() {
			fmt.Fprintln(os.Stderr, "Error: no file specified and stdin is not a pipe")
			fmt.Fprintln(os.Stderr, "Usage: crit plan --name <slug> <file>  or  echo \"content\" | crit plan --name <slug>")
			return nil, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			return nil, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
	}

	if len(strings.TrimSpace(string(content))) == 0 {
		fmt.Fprintln(os.Stderr, "Error: plan content is empty")
		return nil, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}
	return content, nil
}

func resolvePlanSlug(name string, content []byte) string {
	if name != "" {
		return Slugify(name)
	}
	slug := ResolveSlug(content)
	fmt.Fprintf(os.Stderr, "No --name provided, derived slug: %s\n", slug)
	return slug
}

func RunPlan(args []string) error {
	pc := resolvePlanConfig(args)
	content, err := readPlanContent(pc)
	if err != nil {
		return err
	}

	go backgroundCleanup()

	slug := resolvePlanSlug(pc.name, content)
	storageDir, err := PlanStorageDir(slug)
	if err != nil {
		return err
	}

	ver, err := SavePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving plan: %v\n", err)
		return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}

	cwd, _ := daemon.ResolvedCWD()
	key := PlanSessionKey(cwd, slug)
	currentPath := filepath.Join(storageDir, "current.md")
	cfg := config.LoadConfig(cwd)
	noOpenResolved := pc.noOpen || cfg.NoOpen
	quiet := pc.quiet || cfg.Quiet
	if !quiet {
		fmt.Fprintf(os.Stderr, "Plan '%s' saved as v%03d (%d bytes)\n", slug, ver, len(content))
	}
	daemonArgs := BuildPlanDaemonArgs(currentPath, storageDir, slug, PlanDaemonFlags{
		Port:                        config.ResolvePort(pc.port, cfg.Port),
		Host:                        config.ResolveHost(pc.host, cfg.Host),
		PublicURL:                   config.ResolvePublicURL(pc.publicURL, cfg),
		AllowUnauthenticatedNetwork: pc.allowUnauthenticatedNetwork || config.EnvAllowsUnauthenticatedNetwork(),
		NoOpen:                      noOpenResolved,
		Quiet:                       quiet,
		ShareURL:                    config.ResolveShareURL(pc.shareURL, cfg, ""),
	})

	entry, weStartedDaemon, err := connectOrStartDaemon(key, daemonArgs, noOpenResolved, cfg.OpenCmd, quiet)
	if err != nil {
		return err
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := daemon.RunReviewClient(entry, key, quiet)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, config.LoadConfig(cwd).CleanupOnApproveEnabled())
	return nil
}

type planHookEvent struct {
	SessionID string          `json:"session_id"`
	ToolInput json.RawMessage `json:"tool_input"`
}

func resolveHookSlug(sessionID string, content []byte) string {
	if sessionID != "" {
		if existing, ok := LookupPlanSlug(sessionID); ok {
			return existing
		}
		slug := ResolveSlug(content)
		if err := SavePlanSlug(sessionID, slug); err != nil {
			fmt.Fprintf(os.Stderr, "crit plan-hook: warning: could not save slug mapping: %v\n", err)
		}
		return slug
	}
	return ResolveSlug(content)
}

func planApproveModePermissionUpdate(mode string) (map[string]string, bool) {
	switch mode {
	case "default", "manual", "acceptEdits", "plan", "auto", "dontAsk", "bypassPermissions":
		return map[string]string{
			"type":        "setMode",
			"mode":        mode,
			"destination": "session",
		}, true
	default:
		return nil, false
	}
}

func emitHookDecision(approved bool, prompt string, toolInput json.RawMessage, planApproveMode string) {
	if approved {
		decision := map[string]any{
			"behavior":     "allow",
			"updatedInput": toolInput,
		}
		if planApproveMode != "" {
			if update, valid := planApproveModePermissionUpdate(planApproveMode); valid {
				decision["updatedPermissions"] = []map[string]string{update}
			} else {
				fmt.Fprintf(
					os.Stderr,
					"crit plan-hook: warning: ignoring invalid plan_approve_mode %q; expected default, manual, acceptEdits, plan, auto, dontAsk, or bypassPermissions\n",
					planApproveMode,
				)
			}
		}
		out, _ := json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName": "PermissionRequest",
				"decision":      decision,
			},
		})
		fmt.Println(string(out))
		return
	}

	if prompt == "" {
		prompt = "Review comments pending — address them before proceeding."
	}
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior": "deny",
				"message":  prompt,
			},
		},
	})
	fmt.Println(string(out))
}

func emitCodexStopDecision(approved bool, prompt string) {
	if approved {
		return
	}
	if prompt == "" {
		prompt = "Review comments pending — address them before proceeding."
	}
	out, _ := json.Marshal(map[string]any{
		"decision": "block",
		"reason":   prompt,
	})
	fmt.Println(string(out))
}

var runCodexPlanReviewHook = func(sessionID string, content []byte) {
	runPlanReviewHook("crit plan-hook --mode codex", sessionID, content, emitCodexStopDecision)
}

var runClaudePlanReviewHook = func(sessionID string, content []byte, emitDecision func(bool, string)) {
	runPlanReviewHook("crit plan-hook", sessionID, content, emitDecision)
}

func runPlanReviewHook(logPrefix, sessionID string, content []byte, emitDecision func(bool, string)) {
	slug := resolveHookSlug(sessionID, content)

	storageDir, err := PlanStorageDir(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error resolving storage dir: %v\n", logPrefix, err)
		emitDecision(false, fmt.Sprintf("Crit could not prepare plan storage: %v", err))
		return
	}

	ver, err := SavePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error saving plan: %v\n", logPrefix, err)
		emitDecision(false, fmt.Sprintf("Crit could not save the proposed plan: %v", err))
		return
	}
	fmt.Fprintf(os.Stderr, "%s: plan '%s' saved as v%03d\n", logPrefix, slug, ver)

	cwd, _ := daemon.ResolvedCWD()
	cfg := config.LoadConfig(cwd)
	key := PlanSessionKey(cwd, slug)
	currentPath := filepath.Join(storageDir, "current.md")
	daemonArgs := BuildPlanDaemonArgs(currentPath, storageDir, slug, PlanDaemonFlags{
		PublicURL:                   config.ResolvePublicURL("", cfg),
		AllowUnauthenticatedNetwork: config.EnvAllowsUnauthenticatedNetwork(),
	})

	entry, alive := daemon.FindAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "%s: connected to daemon at %s\n", logPrefix, entry.BaseURL())
		if !daemon.DaemonHasBrowser(entry) {
			go browser.OpenBrowserWithCommand(entry.BaseURL(), cfg.OpenCmd)
		}
	} else {
		entry, err = daemon.StartDaemon(key, daemonArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error starting daemon: %v\n", logPrefix, err)
			emitDecision(false, fmt.Sprintf("Crit could not start the review UI: %v", err))
			return
		}
		fmt.Fprintf(os.Stderr, "%s: started daemon at %s (PID %d)\n", logPrefix, entry.BaseURL(), entry.PID)
		weStartedDaemon = true
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved, prompt := daemon.RunReviewClientRaw(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, cfg.CleanupOnApproveEnabled())
	emitDecision(approved, prompt)
}

func automaticPlanReviewDisabled() bool {
	return os.Getenv("CRIT_PLAN_REVIEW") == "off"
}

// RunPlanHook is the PermissionRequest hook handler for ExitPlanMode.
func RunPlanHook() error {
	if automaticPlanReviewDisabled() {
		return nil
	}

	go backgroundCleanup()

	var event planHookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not parse stdin: %v\n", err)
		emitHookDecision(false, "Crit could not parse the plan hook input; plan was not reviewed.", nil, "")
		return nil
	}
	if len(event.ToolInput) == 0 {
		return nil
	}

	var toolInput struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(event.ToolInput, &toolInput); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not parse tool input: %v\n", err)
		emitHookDecision(false, "Crit could not parse the plan hook input; plan was not reviewed.", nil, "")
		return nil
	}
	if strings.TrimSpace(toolInput.Plan) == "" {
		return nil
	}

	emitDecision := func(approved bool, prompt string) {
		planApproveMode := ""
		if approved {
			cwd, _ := daemon.ResolvedCWD()
			// Resolve at approval time because a plan review can stay open for
			// hours and the user may update their global preference meanwhile.
			planApproveMode = config.LoadConfig(cwd).PlanApproveMode
		}
		emitHookDecision(approved, prompt, event.ToolInput, planApproveMode)
	}
	runClaudePlanReviewHook(event.SessionID, []byte(toolInput.Plan), emitDecision)
	return nil
}

// RunCodexPlanHook is the Stop hook handler for Codex proposed-plan review.
func RunCodexPlanHook() error {
	if automaticPlanReviewDisabled() {
		return nil
	}

	var event codexStopHookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not parse stdin: %v\n", err)
		emitCodexStopDecision(false, "Crit could not parse the Codex hook input; plan was not reviewed.")
		return nil
	}
	plan, ok := proposedPlanFromCodexEvent(event)
	if !ok {
		return nil
	}

	go backgroundCleanup()
	runCodexPlanReviewHook(event.SessionID, []byte(plan))
	return nil
}
