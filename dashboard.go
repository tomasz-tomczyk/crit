package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runDashboard is the entry point for the "crit dashboard" subcommand.
func runDashboard(args []string) {
	port := 0
	noOpen := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &port)
			}
		case "--no-open":
			noOpen = true
		}
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	registerDashboardRoutes(mux)

	srv := &http.Server{
		Handler:     mux,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		srv.Close()
		os.Exit(0)
	}()

	fmt.Fprintf(os.Stderr, "Dashboard running on http://localhost:%d\n", actualPort)
	if !noOpen {
		go openBrowser(fmt.Sprintf("http://localhost:%d", actualPort))
	}

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// registerDashboardRoutes sets up HTTP handlers for the dashboard.
func registerDashboardRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("/api/issues", handleDashboardIssues)
	mux.HandleFunc("/api/issues/", handleDashboardIssueBySlug)
	mux.HandleFunc("/api/settings/global", handleDashboardGlobalSettings)
	mux.HandleFunc("/api/settings/project", handleDashboardProjectSettings)

	// Serve dashboard frontend
	mux.HandleFunc("/", serveDashboardHTML)
}

// handleDashboardIssues handles GET (list) and POST (create) for issues.
func handleDashboardIssues(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		states, err := loadAllIssueStates()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(states)

	case http.MethodPost:
		var body struct {
			Description string `json:"description"`
			RepoRoot    string `json:"repo_root"`
			Branch      string `json:"branch,omitempty"`
			Base        string `json:"base,omitempty"`
			OnDone      string `json:"on_done,omitempty"`
			PlanPrompt  string `json:"plan_prompt,omitempty"`
			ExecPrompt  string `json:"exec_prompt,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Description) == "" || strings.TrimSpace(body.RepoRoot) == "" {
			http.Error(w, "description and repo_root required", http.StatusBadRequest)
			return
		}

		// Load config from project
		cfg := LoadConfig(body.RepoRoot)
		if cfg.AgentCmd == "" {
			http.Error(w, "agent_cmd not configured in project", http.StatusBadRequest)
			return
		}

		slug := issueSlug(body.Description)
		branch := body.Branch
		if branch == "" {
			branch = "issue/" + slug
		}
		base := body.Base
		if base == "" {
			base = cfg.BaseBranch
			if base == "" {
				base = "main" // fallback
			}
		}
		onDone := body.OnDone
		if onDone == "" {
			onDone = cfg.OnDone
			if onDone == "" {
				onDone = "pr"
			}
		}

		wtPath := worktreeDir(body.RepoRoot, slug)
		if err := os.MkdirAll(filepath.Dir(wtPath), 0755); err != nil {
			http.Error(w, fmt.Sprintf("creating worktree dir: %v", err), http.StatusInternalServerError)
			return
		}

		// Create worktree (need to run git from the repo root)
		origDir, _ := os.Getwd()
		os.Chdir(body.RepoRoot)
		err := CreateWorktree(base, branch, wtPath)
		os.Chdir(origDir)
		if err != nil {
			http.Error(w, fmt.Sprintf("creating worktree: %v", err), http.StatusInternalServerError)
			return
		}

		state := &issueState{
			Slug:        slug,
			Description: body.Description,
			Branch:      branch,
			Worktree:    wtPath,
			RepoRoot:    body.RepoRoot,
			Base:        base,
			Phase:       "setup",
			OnDone:      onDone,
			PlanPrompt:  body.PlanPrompt,
			ExecPrompt:  body.ExecPrompt,
			AgentCmd:    cfg.AgentCmd,
		}
		if body.PlanPrompt == "" {
			state.PlanPrompt = cfg.PlanPrompt
		}
		if body.ExecPrompt == "" {
			state.ExecPrompt = cfg.ExecPrompt
		}

		if err := saveIssueState(state); err != nil {
			http.Error(w, fmt.Sprintf("saving state: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(state)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDashboardIssueBySlug handles routes like /api/issues/<slug> and /api/issues/<slug>/start
func handleDashboardIssueBySlug(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/issues/<slug>[/action]
	path := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	parts := strings.SplitN(path, "/", 2)
	slug := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if slug == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		// GET /api/issues/:slug — detail view
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)

	case action == "" && r.Method == http.MethodPut:
		// PUT /api/issues/:slug — update issue
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		var body struct {
			Description string `json:"description,omitempty"`
			PlanPrompt  string `json:"plan_prompt,omitempty"`
			ExecPrompt  string `json:"exec_prompt,omitempty"`
			OnDone      string `json:"on_done,omitempty"`
			Branch      string `json:"branch,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if body.Description != "" {
			state.Description = body.Description
		}
		if body.PlanPrompt != "" {
			state.PlanPrompt = body.PlanPrompt
		}
		if body.ExecPrompt != "" {
			state.ExecPrompt = body.ExecPrompt
		}
		if body.OnDone != "" {
			state.OnDone = body.OnDone
		}
		if body.Branch != "" && state.Phase == "setup" {
			state.Branch = body.Branch
		}
		if err := saveIssueState(state); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)

	case action == "" && r.Method == http.MethodDelete:
		// DELETE /api/issues/:slug — abort + cleanup
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Kill daemon if running
		if state.DaemonPort > 0 {
			// Try to find and kill the daemon
			key := sessionKey(state.Worktree, nil)
			if entry, alive := findAliveSession(key); alive {
				killDaemon(entry)
			}
		}
		// Remove worktree
		origDir, _ := os.Getwd()
		os.Chdir(state.RepoRoot)
		_ = RemoveWorktree(state.Worktree)
		os.Chdir(origDir)
		// Remove state file
		_ = deleteIssueState(state.RepoRoot, state.Slug)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	case action == "settings" && r.Method == http.MethodPut:
		// PUT /api/issues/:slug/settings — update issue-specific overrides
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		var body struct {
			PlanPrompt string `json:"plan_prompt"`
			ExecPrompt string `json:"exec_prompt"`
			OnDone     string `json:"on_done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		state.PlanPrompt = body.PlanPrompt
		state.ExecPrompt = body.ExecPrompt
		if body.OnDone != "" {
			state.OnDone = body.OnDone
		}
		if err := saveIssueState(state); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)

	case action == "start" && r.Method == http.MethodPost:
		// POST /api/issues/:slug/start — trigger next phase
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		switch state.Phase {
		case "setup":
			// Trigger planning asynchronously
			go runPlanningPhase(state)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "planning_started", "phase": "planning"})
		case "executing":
			// Trigger execution asynchronously
			go runExecutionPhase(state)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "execution_started", "phase": "executing"})
		default:
			http.Error(w, fmt.Sprintf("cannot start from phase %q", state.Phase), http.StatusBadRequest)
		}

	case action == "refine" && r.Method == http.MethodPost:
		// POST /api/issues/:slug/refine — trigger refinement
		state, err := loadIssueState(slug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if state.Phase != "setup" {
			http.Error(w, "can only refine in setup phase", http.StatusBadRequest)
			return
		}
		go runRefinePhase(state)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "refining_started"})

	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleDashboardGlobalSettings handles GET/PUT for global config (~/.crit.config.json).
func handleDashboardGlobalSettings(w http.ResponseWriter, r *http.Request) {
	globalPath := globalConfigPath()

	switch r.Method {
	case http.MethodGet:
		cfg, _, err := loadConfigFile(globalPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)

	case http.MethodPut:
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(globalPath, data, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDashboardProjectSettings handles GET/PUT for project config.
// Requires ?project=<repo-root> query param.
func handleDashboardProjectSettings(w http.ResponseWriter, r *http.Request) {
	projectDir := r.URL.Query().Get("project")
	if projectDir == "" {
		http.Error(w, "project query param required", http.StatusBadRequest)
		return
	}
	projectPath := filepath.Join(projectDir, ".crit.config.json")

	switch r.Method {
	case http.MethodGet:
		cfg, _, err := loadConfigFile(projectPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)

	case http.MethodPut:
		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(projectPath, data, 0644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// serveDashboardHTML serves the embedded dashboard HTML.
func serveDashboardHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := frontendFS.ReadFile("frontend/dashboard.html")
	if err != nil {
		// Dashboard HTML not yet embedded — serve inline fallback
		w.Header().Set("Content-Type", "text/html")
		w.Write(dashboardHTMLFallback())
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(data)
}

// dashboardHTMLFallback returns a minimal inline HTML for the dashboard
// when the embedded file isn't available (development).
func dashboardHTMLFallback() []byte {
	return []byte(`<!DOCTYPE html>
<html><head><title>crit dashboard</title></head>
<body><h1>crit dashboard</h1><p>Dashboard frontend not yet built. Use the API directly.</p>
<script>
fetch('/api/issues').then(r=>r.json()).then(d=>{
  document.body.innerHTML += '<pre>'+JSON.stringify(d,null,2)+'</pre>';
});
</script></body></html>`)
}
