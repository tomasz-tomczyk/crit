package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// issueState tracks the lifecycle of a crit issue workflow.
// Persisted to ~/.crit/issues/<project-hash>-<slug>.json.
type issueState struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"` // longer context; populated by refinement
	Branch      string `json:"branch"`
	Worktree    string `json:"worktree"`
	RepoRoot    string `json:"repo_root"`
	Base        string `json:"base"`
	Phase       string `json:"phase"`                   // setup, refining, planning, plan-review, approved, executing, code-review, done, error
	ErrMsg      string `json:"error_msg,omitempty"`      // set when phase=error
	AutoMode    bool   `json:"auto_mode,omitempty"`      // true when --go mode was used
	OnDone      string `json:"on_done"`
	PlanPrompt  string `json:"plan_prompt,omitempty"`
	ExecPrompt  string `json:"exec_prompt,omitempty"`
	AgentCmd    string `json:"agent_cmd"`
	DaemonPort  int    `json:"daemon_port,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// issueConfig holds parsed CLI options for crit issue.
type issueConfig struct {
	description string
	file        string // --file: read description from file
	resume      string // --resume: slug of existing issue
	refine      string // --refine: slug of issue to refine
	plan        string // --plan: slug of issue to start planning
	execute     string // --execute: slug of issue to start executing
	auto        bool   // --go: run full cycle automatically
}

// issuesDir returns the path to ~/.crit/issues/.
func issuesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	dir := filepath.Join(home, ".crit", "issues")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating issues directory: %w", err)
	}
	return dir, nil
}

// worktreeDir returns the path for a worktree: ~/.crit/worktrees/<project-hash>/<slug>/
func worktreeDir(repoRoot, slug string) string {
	home, _ := os.UserHomeDir()
	h := sha256.Sum256([]byte(repoRoot))
	projectHash := fmt.Sprintf("%x", h[:])[:8]
	return filepath.Join(home, ".crit", "worktrees", projectHash, slug)
}

// issueStateFile returns the path for an issue state file.
func issueStateFile(repoRoot, slug string) (string, error) {
	dir, err := issuesDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(repoRoot))
	projectHash := fmt.Sprintf("%x", h[:])[:8]
	return filepath.Join(dir, projectHash+"-"+slug+".json"), nil
}

// saveIssueState persists issue state to disk.
func saveIssueState(state *issueState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if state.CreatedAt == "" {
		state.CreatedAt = state.UpdatedAt
	}
	path, err := issueStateFile(state.RepoRoot, state.Slug)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling issue state: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	dashboardSSE.broadcast("issues-changed", "")
	return nil
}

// loadIssueState loads issue state from disk by slug.
// Searches all issue files for a matching slug.
func loadIssueState(slug string) (*issueState, error) {
	dir, err := issuesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading issues directory: %w", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var state issueState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if state.Slug == slug {
			return &state, nil
		}
	}
	return nil, fmt.Errorf("issue %q not found", slug)
}

// loadAllIssueStates returns all issue states from ~/.crit/issues/.
func loadAllIssueStates() ([]*issueState, error) {
	dir, err := issuesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading issues directory: %w", err)
	}
	var states []*issueState
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var state issueState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		states = append(states, &state)
	}
	// Sort by updated_at descending (most recent first)
	sort.Slice(states, func(i, j int) bool {
		return states[i].UpdatedAt > states[j].UpdatedAt
	})
	return states, nil
}

// deleteIssueState removes an issue state file from disk.
func deleteIssueState(repoRoot, slug string) error {
	path, err := issueStateFile(repoRoot, slug)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	dashboardSSE.broadcast("issues-changed", "")
	return nil
}

// resolveIssueConfig parses CLI arguments for crit issue.
func resolveIssueConfig(args []string) issueConfig {
	var cfg issueConfig
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--file" || arg == "-f":
			if i+1 < len(args) {
				i++
				cfg.file = args[i]
			}
		case arg == "--resume":
			if i+1 < len(args) {
				i++
				cfg.resume = args[i]
			}
		case arg == "--refine":
			if i+1 < len(args) {
				i++
				cfg.refine = args[i]
			}
		case arg == "--plan":
			if i+1 < len(args) {
				i++
				cfg.plan = args[i]
			}
		case arg == "--execute":
			if i+1 < len(args) {
				i++
				cfg.execute = args[i]
			}
		case arg == "--go":
			cfg.auto = true
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) > 0 {
		cfg.description = strings.Join(positional, " ")
	}

	return cfg
}

// runIssue is the entry point for the "crit issue" subcommand.
func runIssue(args []string) {
	cfg := resolveIssueConfig(args)

	// Handle phase-advancement commands on existing issues
	if cfg.resume != "" {
		state, err := loadIssueState(cfg.resume)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		runIssueFromPhase(state)
		return
	}
	if cfg.refine != "" {
		state, err := loadIssueState(cfg.refine)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := runRefinePhase(state); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if cfg.plan != "" {
		state, err := loadIssueState(cfg.plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := runPlanningPhase(state); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if cfg.execute != "" {
		state, err := loadIssueState(cfg.execute)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := runExecutionPhase(state); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Creating a new issue
	if !IsGitRepo() {
		fmt.Fprintln(os.Stderr, "Error: crit issue requires a git repository")
		os.Exit(1)
	}

	repoRoot, err := RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	loadedCfg := LoadConfig(repoRoot)
	if loadedCfg.AgentCmd == "" {
		fmt.Fprintln(os.Stderr, "Error: crit issue requires agent_cmd in config")
		fmt.Fprintln(os.Stderr, "Set it in .crit.config.json or ~/.crit.config.json:")
		fmt.Fprintln(os.Stderr, `  { "agent_cmd": "claude -p" }`)
		os.Exit(1)
	}

	// Read description
	description := cfg.description
	if cfg.file != "" {
		data, err := os.ReadFile(cfg.file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", cfg.file, err)
			os.Exit(1)
		}
		description = string(data)
	}
	if strings.TrimSpace(description) == "" {
		fmt.Fprintln(os.Stderr, "Error: issue description required")
		fmt.Fprintln(os.Stderr, "Usage: crit issue \"description\"")
		fmt.Fprintln(os.Stderr, "       crit issue --file description.md")
		os.Exit(1)
	}

	// Derive slug and branch
	slug := issueSlug(description)
	branch := "issue/" + slug

	// Detect base branch
	base := loadedCfg.BaseBranch
	if base == "" {
		base = DefaultBranch()
	}

	// Create worktree
	wtPath := worktreeDir(repoRoot, slug)
	if err := os.MkdirAll(filepath.Dir(wtPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating worktree directory: %v\n", err)
		os.Exit(1)
	}

	if err := CreateWorktree(base, branch, wtPath, repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Determine on_done strategy
	onDone := loadedCfg.OnDone
	if onDone == "" {
		onDone = "pr"
	}

	// Save initial state
	state := &issueState{
		Slug:        slug,
		Title:       description, // user's input is the title
		Description: "",          // populated by refinement
		Branch:      branch,
		Worktree:    wtPath,
		RepoRoot:    repoRoot,
		Base:        base,
		Phase:       "setup",
		AutoMode:    cfg.auto,
		OnDone:      onDone,
		PlanPrompt:  loadedCfg.PlanPrompt,
		ExecPrompt:  loadedCfg.ExecPrompt,
		AgentCmd:    loadedCfg.AgentCmd,
	}
	if err := saveIssueState(state); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving issue state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Issue '%s' created\n", slug)
	fmt.Fprintf(os.Stderr, "  Worktree: %s\n", wtPath)
	fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)

	// --go mode: run full cycle automatically
	if cfg.auto {
		if err := runPlanningPhase(state); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// After runPlanningPhase -> runPlanReviewPhase, state.Phase is "approved"
		if state.Phase == "approved" {
			if err := runExecutionPhase(state); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	fmt.Fprintf(os.Stderr, "\nTo start planning: crit issue --plan %s\n", slug)
	fmt.Fprintf(os.Stderr, "To refine first:   crit issue --refine %s\n", slug)
	fmt.Fprintf(os.Stderr, "Or open dashboard: crit dashboard\n")
}

// issueSlug derives a slug from the first line of the description + date.
func issueSlug(description string) string {
	date := time.Now().Format("2006-01-02")
	// Use first line or first 50 runes (safe for unicode)
	line := strings.SplitN(description, "\n", 2)[0]
	runes := []rune(line)
	if len(runes) > 50 {
		line = string(runes[:50])
	}
	return slugify(line) + "-" + date
}

// runIssueFromPhase resumes an issue from its current saved phase.
func runIssueFromPhase(state *issueState) {
	var err error
	switch state.Phase {
	case "setup":
		fmt.Fprintf(os.Stderr, "Issue '%s' is in setup phase.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "To start planning: crit issue --plan %s\n", state.Slug)
		return
	case "refining":
		fmt.Fprintf(os.Stderr, "Issue '%s' was interrupted during refining. Restarting...\n", state.Slug)
		err = runRefinePhase(state)
	case "planning":
		fmt.Fprintf(os.Stderr, "Issue '%s' was interrupted during planning. Restarting...\n", state.Slug)
		err = runPlanningPhase(state)
	case "plan-review":
		err = runPlanReviewPhase(state)
	case "approved":
		fmt.Fprintf(os.Stderr, "Issue '%s' is ready for execution.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "To execute: crit issue --execute %s\n", state.Slug)
		return
	case "executing":
		fmt.Fprintf(os.Stderr, "Issue '%s' is currently executing.\n", state.Slug)
		return
	case "code-review":
		err = runCodeReviewPhase(state)
	case "done":
		fmt.Fprintf(os.Stderr, "Issue '%s' is already done.\n", state.Slug)
		return
	default:
		fmt.Fprintf(os.Stderr, "Issue '%s' is in unknown phase: %s\n", state.Slug, state.Phase)
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runRefinePhase invokes the agent to refine/expand the issue description.
func runRefinePhase(state *issueState) error {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "You are working in: %s\n\n", state.Worktree)
	fmt.Fprintf(&prompt, "Refine the following issue. Research the codebase to add technical context,\n")
	fmt.Fprintf(&prompt, "identify affected files, and clarify the scope of the change.\n\n")
	fmt.Fprintf(&prompt, "Respond with a JSON object with exactly two fields:\n\n")
	fmt.Fprintf(&prompt, "  title       — a short, clear title (plain text, one line, no markdown, no heading prefix)\n")
	fmt.Fprintf(&prompt, "  description — a detailed markdown description. Must NOT repeat the title.\n")
	fmt.Fprintf(&prompt, "                Start directly with content (## Problem, ## Context, etc.).\n\n")
	fmt.Fprintf(&prompt, "Example:\n")
	fmt.Fprintf(&prompt, "{\n")
	fmt.Fprintf(&prompt, "  \"title\": \"Fix login timeout on mobile Safari\",\n")
	fmt.Fprintf(&prompt, "  \"description\": \"## Problem\\nSessions expire after 5 minutes on iOS...\\n\\n## Affected files\\n- `auth/session.go`\"\n")
	fmt.Fprintf(&prompt, "}\n\n")
	fmt.Fprintf(&prompt, "Rules:\n")
	fmt.Fprintf(&prompt, "- Output ONLY the JSON object, nothing else\n")
	fmt.Fprintf(&prompt, "- description must not start with a heading that repeats the title\n")
	fmt.Fprintf(&prompt, "- title must be plain text (no #, no **, no backticks)\n\n")
	fmt.Fprintf(&prompt, "## Issue title\n%s\n\n", state.Title)
	if state.Description != "" {
		fmt.Fprintf(&prompt, "## Additional context\n%s\n", state.Description)
	}

	state.Phase = "refining"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Refining issue with %s...\n", agentName(state.AgentCmd))
	stdout, err := invokeAgentLogged(state.Slug, state.AgentCmd, prompt.String(), state.Worktree)
	if err != nil {
		setPhaseError(state, err.Error())
		return err
	}

	if refined := parseRefineOutput(stdout); refined != nil {
		if refined.Title != "" {
			state.Title = refined.Title
		}
		if refined.Description != "" {
			state.Description = refined.Description
		}
	}
	state.Phase = "setup"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Issue refined. To start planning: crit issue --plan %s\n", state.Slug)
	return nil
}

// parseRefineOutput extracts title+description from agent JSON output.
// Handles output wrapped in ```json ... ``` code fences.
func parseRefineOutput(output string) *struct{ Title, Description string } {
	s := strings.TrimSpace(output)
	// Strip code fence if present
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	var result struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	// Strip any leading heading from description — title is stored as its own field.
	desc := strings.TrimSpace(result.Description)
	if first, rest, found := strings.Cut(desc, "\n"); found && strings.HasPrefix(first, "#") {
		desc = strings.TrimSpace(rest)
	} else if strings.HasPrefix(desc, "#") {
		desc = ""
	}
	result.Description = desc
	return &struct{ Title, Description string }{result.Title, result.Description}
}

// runPlanningPhase invokes the agent to create a plan, then transitions to plan review.
func runPlanningPhase(state *issueState) error {
	var prompt strings.Builder
	// Layer 1: crit context
	fmt.Fprintf(&prompt, "You are working in: %s\n\n", state.Worktree)
	fmt.Fprintf(&prompt, "Create a detailed implementation plan for the issue below.\n\n")
	fmt.Fprintf(&prompt, "Output the plan as markdown. Include:\n")
	fmt.Fprintf(&prompt, "- Step-by-step implementation plan\n")
	fmt.Fprintf(&prompt, "- Files to create or modify\n")
	fmt.Fprintf(&prompt, "- Key design decisions\n")
	fmt.Fprintf(&prompt, "- Edge cases to handle\n\n")
	fmt.Fprintf(&prompt, "Do NOT implement anything yet — only plan.\n")
	fmt.Fprintf(&prompt, "Output ONLY the plan markdown to stdout.\n\n")

	// Layer 2: user's custom planning prompt
	if state.PlanPrompt != "" {
		fmt.Fprintf(&prompt, "Additional instructions:\n%s\n\n", state.PlanPrompt)
	}

	// Layer 3: the issue description
	fmt.Fprintf(&prompt, "## Issue\n%s\n", state.Description)

	state.Phase = "planning"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Planning with %s...\n", agentName(state.AgentCmd))
	stdout, err := invokeAgentLogged(state.Slug, state.AgentCmd, prompt.String(), state.Worktree)
	if err != nil {
		setPhaseError(state, err.Error())
		return err
	}

	if strings.TrimSpace(stdout) == "" {
		setPhaseError(state, "agent produced empty plan")
		return fmt.Errorf("agent produced empty plan")
	}

	// Store plan via existing crit plan infrastructure
	slug := slugify(state.Slug)
	storageDir := planStorageDir(slug)
	ver, err := savePlanVersion(storageDir, []byte(stdout))
	if err != nil {
		setPhaseError(state, fmt.Sprintf("saving plan: %v", err))
		return fmt.Errorf("saving plan: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Plan saved as v%03d\n", ver)

	state.Phase = "plan-review"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	// Automatically enter plan review
	return runPlanReviewPhase(state)
}

// runPlanReviewPhase opens a crit plan daemon and runs the review/feedback loop.
func runPlanReviewPhase(state *issueState) error {
	slug := slugify(state.Slug)
	storageDir := planStorageDir(slug)
	currentPath := filepath.Join(storageDir, "current.md")

	// Check plan exists
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		msg := fmt.Sprintf("no plan found. Run: crit issue --plan %s", state.Slug)
		setPhaseError(state, msg)
		return fmt.Errorf("%s", msg)
	}

	daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, 0, false, false)

	key := planSessionKey(state.RepoRoot, slug)

	entry, alive := findAliveSession(key)
	if !alive {
		var err error
		entry, err = startDaemon(key, daemonArgs)
		if err != nil {
			setPhaseError(state, fmt.Sprintf("starting daemon: %v", err))
			return fmt.Errorf("starting daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Started plan review on port %d\n", entry.Port)
	} else {
		fmt.Fprintf(os.Stderr, "Connected to plan review on port %d\n", entry.Port)
	}

	state.DaemonPort = entry.Port
	_ = saveIssueState(state)

	// Review/feedback loop
	for {
		approved, feedback := runReviewClientRaw(entry)
		if approved {
			killDaemon(entry)
			state.Phase = "approved"
			state.DaemonPort = 0
			_ = saveIssueState(state)
			fmt.Fprintf(os.Stderr, "Plan approved! To execute: crit issue --execute %s\n", state.Slug)
			return nil
		}

		// Pipe feedback to agent — agent outputs updated plan to stdout
		fmt.Fprintf(os.Stderr, "Sending feedback to %s...\n", agentName(state.AgentCmd))
		stdout, err := invokeAgentLogged(state.Slug, state.AgentCmd, feedback, state.Worktree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent error: %v\n", err)
			fmt.Fprintln(os.Stderr, "Continuing review with current plan...")
			continue
		}

		if strings.TrimSpace(stdout) != "" {
			// Save new plan version
			ver, err := savePlanVersion(storageDir, []byte(stdout))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error saving plan update: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "Plan updated to v%03d\n", ver)
		}

		// Signal round-complete so plan daemon picks up the new version
		signalRoundComplete(entry.Port)
	}
}

// runExecutionPhase invokes the agent to implement the approved plan.
func runExecutionPhase(state *issueState) error {
	slug := slugify(state.Slug)
	storageDir := planStorageDir(slug)
	currentPath := filepath.Join(storageDir, "current.md")

	planContent, err := os.ReadFile(currentPath)
	if err != nil {
		setPhaseError(state, fmt.Sprintf("reading plan: %v", err))
		return fmt.Errorf("reading plan: %w", err)
	}

	var prompt strings.Builder
	// Layer 1: crit context
	fmt.Fprintf(&prompt, "You are working in: %s\n\n", state.Worktree)
	fmt.Fprintf(&prompt, "Execute the following plan. Make all necessary code changes.\n")
	fmt.Fprintf(&prompt, "Commit your changes with clear commit messages as you go.\n\n")

	// Layer 2: user's custom execution prompt
	if state.ExecPrompt != "" {
		fmt.Fprintf(&prompt, "Additional instructions:\n%s\n\n", state.ExecPrompt)
	}

	// Layer 3: the approved plan
	fmt.Fprintf(&prompt, "## Plan\n%s\n", string(planContent))

	// Layer 4: carry forward unresolved plan review comments
	critJSONPath := filepath.Join(storageDir, ".crit.json")
	var reviewNotes strings.Builder
	if data, readErr := os.ReadFile(critJSONPath); readErr == nil {
		var critData struct {
			Files map[string]struct {
				Comments []struct {
					Body     string `json:"body"`
					Resolved bool   `json:"resolved"`
					Replies  []struct {
						Body string `json:"body"`
					} `json:"replies"`
				} `json:"comments"`
			} `json:"files"`
		}
		if json.Unmarshal(data, &critData) == nil {
			for _, fileData := range critData.Files {
				for _, comment := range fileData.Comments {
					if !comment.Resolved {
						fmt.Fprintf(&reviewNotes, "- %s\n", comment.Body)
					}
				}
			}
		}
	}
	if reviewNotes.Len() > 0 {
		fmt.Fprintf(&prompt, "## Reviewer concerns from plan review (must address)\n%s\n", reviewNotes.String())
	}

	state.Phase = "executing"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Executing plan with %s...\n", agentName(state.AgentCmd))
	_, err = invokeAgentLogged(state.Slug, state.AgentCmd, prompt.String(), state.Worktree)
	if err != nil {
		setPhaseError(state, err.Error())
		return err
	}

	state.Phase = "code-review"
	if err := saveIssueState(state); err != nil {
		setPhaseError(state, fmt.Sprintf("saving state: %v", err))
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Execution complete. Starting code review...")
	return runCodeReviewPhase(state)
}

// runCodeReviewPhase starts a crit daemon in git mode in the worktree for code review.
func runCodeReviewPhase(state *issueState) error {
	key := sessionKey(state.Worktree, nil)

	entry, alive := findAliveSession(key)
	if !alive {
		var err error
		entry, err = startDaemonInDir(key, nil, state.Worktree)
		if err != nil {
			setPhaseError(state, fmt.Sprintf("starting code review daemon: %v", err))
			return fmt.Errorf("starting code review daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Started code review on port %d\n", entry.Port)
	} else {
		fmt.Fprintf(os.Stderr, "Connected to code review on port %d\n", entry.Port)
	}

	state.DaemonPort = entry.Port
	_ = saveIssueState(state)

	// Review/feedback loop
	for {
		approved, feedback := runReviewClientRaw(entry)
		if approved {
			killDaemon(entry)
			state.Phase = "done"
			state.DaemonPort = 0
			_ = saveIssueState(state)
			runCompletionPhase(state)
			return nil
		}

		// Pipe feedback to agent for iteration
		fmt.Fprintf(os.Stderr, "Sending feedback to %s...\n", agentName(state.AgentCmd))
		_, err := invokeAgentLogged(state.Slug, state.AgentCmd, feedback, state.Worktree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent error: %v\n", err)
			fmt.Fprintln(os.Stderr, "Continuing review...")
			continue
		}

		// Signal round-complete so daemon picks up changes
		signalRoundComplete(entry.Port)
	}
}

// runCompletionPhase handles the post-approval workflow.
func runCompletionPhase(state *issueState) {
	switch state.OnDone {
	case "pr":
		fmt.Fprintf(os.Stderr, "\nIssue '%s' approved.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "Branch: %s\n", state.Branch)
		fmt.Fprintf(os.Stderr, "Worktree: %s\n\n", state.Worktree)
		fmt.Fprintln(os.Stderr, "To create a PR:")
		fmt.Fprintf(os.Stderr, "  cd %s && gh pr create --head %s\n", state.Worktree, state.Branch)
	case "merge":
		fmt.Fprintf(os.Stderr, "\nIssue '%s' approved.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "To merge into %s:\n", state.Base)
		fmt.Fprintf(os.Stderr, "  git merge %s\n", state.Branch)
		fmt.Fprintf(os.Stderr, "Then clean up worktree:\n")
		fmt.Fprintf(os.Stderr, "  git worktree remove %s\n", state.Worktree)
	case "none":
		fmt.Fprintf(os.Stderr, "\nIssue '%s' approved.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "Worktree: %s\n", state.Worktree)
		fmt.Fprintf(os.Stderr, "Branch: %s\n", state.Branch)
	default:
		fmt.Fprintf(os.Stderr, "\nIssue '%s' approved.\n", state.Slug)
		fmt.Fprintf(os.Stderr, "Branch: %s\n", state.Branch)
	}
}

// signalRoundComplete sends a round-complete signal to a running daemon.
func signalRoundComplete(port int) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/api/round-complete", port),
		"application/json",
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not signal round-complete: %v\n", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

// killDaemon gracefully stops a daemon process (SIGTERM, then SIGKILL after 2s).
func killDaemon(entry sessionEntry) {
	proc, err := os.FindProcess(entry.PID)
	if err != nil {
		return
	}
	// Try graceful shutdown first
	proc.Signal(syscall.SIGTERM)
	// Give it 2 seconds, then force
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				close(done)
				return
			}
		}
		close(done)
	}()
	select {
	case <-done:
		// process exited cleanly
	case <-time.After(2 * time.Second):
		proc.Signal(os.Kill)
	}
}

// setPhaseError sets the issue phase to "error" with a message and persists state.
func setPhaseError(state *issueState, msg string) {
	state.Phase = "error"
	state.ErrMsg = msg
	state.DaemonPort = 0
	_ = saveIssueState(state)
	fmt.Fprintf(os.Stderr, "Error in phase: %s\n", msg)
}

// issueLog is a ring buffer that stores the last 200 lines of agent output for an issue.
type issueLog struct {
	mu    sync.Mutex
	lines []string
	subs  map[chan string]struct{}
}

func (l *issueLog) write(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
	if len(l.lines) > 200 {
		l.lines = l.lines[len(l.lines)-200:]
	}
	for ch := range l.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (l *issueLog) subscribe() ([]string, chan string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan string, 32)
	l.subs[ch] = struct{}{}
	// Return copy of existing lines so subscriber gets backlog
	snapshot := make([]string, len(l.lines))
	copy(snapshot, l.lines)
	return snapshot, ch
}

func (l *issueLog) unsubscribe(ch chan string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.subs, ch)
	close(ch)
}

var issueLogs sync.Map // slug -> *issueLog

func issueLogFor(slug string) *issueLog {
	v, _ := issueLogs.LoadOrStore(slug, &issueLog{subs: make(map[chan string]struct{})})
	return v.(*issueLog)
}

// invokeAgentLogged invokes the agent and streams stderr output to the issue log.
func invokeAgentLogged(slug, agentCmd, prompt, cwd string) (string, error) {
	log := issueLogFor(slug)
	outputCh := make(chan string, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for line := range outputCh {
			log.write(line)
		}
	}()
	stdout, err := invokeAgentWithOutput(agentCmd, prompt, cwd, outputCh)
	close(outputCh)
	<-done // wait for all lines to be written
	return stdout, err
}
