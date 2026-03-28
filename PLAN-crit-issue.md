# `crit issue` — Higher-Order Orchestration

## Context

Today, crit operates at the **review** level: you already have changes (or a plan), and crit helps a human review them. The `crit issue` command lifts crit to the **task** level — starting from a problem description and orchestrating the full lifecycle: planning, plan review, execution, and code review — all in an isolated git worktree.

This bridges the gap between "I have an issue" and "I have reviewed, approved code." Crit becomes the outer loop that drives the agent, rather than being a tool the agent calls.

Future: sync with GitHub Issues as input, and create PRs as output.

## State Machine

```
  crit issue        ┌────────┐          ┌──────────────────────────────────────────┐
  "desc..." ──► SETUP ──► │ REFINE │──┐       │                                          │
                  │       │(optional)│  │       │                                          │
                  │       └────────┘  ▼       │                                          │
                  │                 ┌──────────┐  ┌──────────────┐  ┌───────────┐  ┌─────────────┐
                  └────────────────►│ PLANNING │─►│ PLAN REVIEW  │─►│ EXECUTING │─►│ CODE REVIEW │──► DONE
                                    └──────────┘  └──────┬───────┘  └─────┬─────┘  └──────┬──────┘
                                         ▲               │                │               │
                                         └── feedback ───┘                └── feedback ───┘

  ~/.crit/issues/   state file       ~/.crit/plans/   plan versions       worktree   code changes
```

**Phases:**
1. **SETUP** — Create worktree + branch, store issue description in `~/.crit/issues/`. **Stops here.** User must explicitly trigger the next phase.
2. **REFINE** *(opt-in)* — Only triggered by `crit issue --refine <slug>` or "Refine" button in dashboard. Invokes `agent_cmd` to expand the issue description with codebase research and context. Not part of the default flow.
3. **PLANNING** — Explicitly triggered. Invoke `agent_cmd` with planning prompt. Agent outputs plan, stored via `crit plan` infrastructure in `~/.crit/plans/`.
4. **PLAN REVIEW** — Open `crit plan` on the plan, human reviews/comments, loop with agent until approved.
5. **EXECUTING** — Explicitly triggered after plan approval. Invoke `agent_cmd` with execution prompt, agent makes code changes in worktree.
6. **CODE REVIEW** — Switch to `crit` (git mode) in worktree, human reviews code, loop with agent until approved.
7. **DONE** — Configurable: create PR, merge to original branch, or just print next steps.

**Explicit triggers:** The transitions setup→refine, setup→planning, and plan-review→executing require an explicit action (CLI flag, dashboard button, or interactive prompt). This lets users inspect, tweak prompts, or edit manually before launching the agent.

**Plan storage:** Plans live in `~/.crit/plans/<slug>/` (existing infrastructure), NOT in the worktree. The worktree is purely for code. This leverages the existing `crit plan` versioning system and keeps the worktree clean.

## CLI Interface

```bash
# Create an issue (setup only — does NOT auto-launch planning)
crit issue "Add rate limiting to the API endpoints"

# From a file
crit issue --file issue-description.md

# Future: from GitHub issue
crit issue --gh 42

# Resume / advance an existing issue
crit issue --resume rate-limiting-api     # picks up from last saved phase
crit issue --refine rate-limiting-api     # agent refines the issue description
crit issue --plan rate-limiting-api       # trigger planning phase
crit issue --execute rate-limiting-api    # trigger execution phase
```

**Minimal CLI flags.** Everything else (branch name, base branch, on-done strategy, custom prompts) is configured via the **dashboard settings screen** or by editing the issue state file directly. This keeps the CLI simple. The only CLI args are:
- Positional: issue description (for creation)
- `--file`: read description from file
- `--gh`: GitHub issue number (future)
- `--resume`, `--refine`, `--plan`, `--execute`: advance existing issues

### Custom Prompts

Configured via dashboard settings or issue state file (not CLI flags). Crit always injects its own context, and custom prompts are appended.

**Prompt structure (all phases):**
```
[crit-injected context: worktree path, phase instructions, file references]
[user's custom prompt for this phase]
[content: issue description (planning) or approved plan (execution)]
```

**Three levels of prompt config** (higher overrides lower):
1. **Issue-specific** — in `~/.crit/issues/<slug>.json` (editable via dashboard)
2. **Project-level** — in `.crit.config.json`
3. **Global** — in `~/.crit.config.json`

```json
// .crit.config.json
{
  "agent_cmd": "claude -p",
  "plan_prompt": "Always consider backward compatibility",
  "exec_prompt": "Follow existing code style, add tests for new functions",
  "on_done": "pr"
}
```

## Files to Create/Modify

### New: `issue.go`
Main orchestration logic:
- `runIssue(args []string)` — CLI entry point, parses flags, drives the state machine
- `issueConfig` struct — parsed CLI options (description, branch, base, on-done, plan-prompt, exec-prompt, start, resume, etc.)
- `resolveIssueConfig(args)` — flag parsing (follows pattern from `resolvePlanConfig`)
- `setupWorktree(base, branch string) (worktreePath string, err error)` — creates worktree
- `cleanupWorktree(worktreePath string)` — removes worktree (on error/abort)
- `seedPlanFile(worktreePath, description string) string` — writes initial `plan.md`
- `runPlanningPhase(cfg, worktreePath)` — invokes agent to write the plan
- `runPlanReviewLoop(cfg, worktreePath, entry)` — review + feedback loop
- `runExecutionPhase(cfg, worktreePath, planContent)` — invokes agent to implement
- `runCodeReviewLoop(cfg, worktreePath, entry)` — review + feedback loop
- `runCompletionPhase(cfg, worktreePath, branch)` — PR/merge/print
- `issueState` struct + `saveIssueState`/`loadIssueState` for persistence/resume

### New: `agent.go`
Extracted shared agent invocation logic (currently inline in `server.go`):
- `invokeAgent(cmd, prompt, cwd string) (stdout string, err error)`
- Supports `{prompt}` placeholder and stdin pipe modes
- Used by both `server.go` (per-comment agent requests) and `issue.go` (phase orchestration)

### New: `issue_test.go`
Tests for config parsing, worktree management, state persistence, phase transitions.

### New (follow-up): `dashboard.go`
Dashboard daemon for issue management UI.

### New (follow-up): `frontend/dashboard.html` + `frontend/dashboard.js`
Dashboard frontend.

### Modify: `main.go`
- Add `case "issue":` → `runIssue(os.Args[2:])`
- Add `case "dashboard":` → `runDashboard(os.Args[2:])` (follow-up)
- Update `printHelp()`

### Modify: `config.go`
Add fields to Config:
- `OnDone string` (default: `"pr"`) — completion strategy
- `PlanPrompt string` — default custom prompt for planning phase
- `ExecPrompt string` — default custom prompt for execution phase

### Modify: `git.go`
Add worktree helpers:
- `CreateWorktree(base, branch, path string) error` — `git worktree add -b <branch> <path> <base>`
- `RemoveWorktree(path string) error` — `git worktree remove <path>`
- `WorktreeList() ([]string, error)` — `git worktree list`

### Modify: `daemon.go`
- Add `startDaemonInDir(key string, args []string, cwd string)` variant (sets `cmd.Dir`)

### Modify: `session.go`
- Add `SwitchToGitMode(baseRef string)` method
- Add `StopWatching()` to cleanly stop file/git watchers before mode switch

### Modify: `server.go`
- Add `POST /api/session/switch-mode` endpoint
- Extract agent invocation into `agent.go` (reduce duplication)

### Modify: `frontend/app.js`
- Handle `mode-switched` SSE event → call `loadSession()` to re-render

## Detailed Phase Design

### Phase 1: SETUP

```go
func runIssue(args []string) {
    cfg := resolveIssueConfig(args)

    // Handle --resume: load existing state and jump to saved phase
    if cfg.resume != "" {
        state := loadIssueState(cfg.resume)
        runIssueFromPhase(cfg, state)
        return
    }

    // Validate: must be in a git repo, agent_cmd must be configured
    if !IsGitRepo() { fatal("crit issue requires a git repository") }
    agentCmd := resolveAgentCmd(cfg)
    if agentCmd == "" { fatal("crit issue requires agent_cmd in config") }

    // Create worktree
    base := cfg.base // or auto-detect default branch
    branch := cfg.branch // or auto-generate: "issue/<slug>"
    home, _ := os.UserHomeDir()
    projectHash := sha256hex(repoRoot)[:8]
    worktreePath := filepath.Join(home, ".crit", "worktrees", projectHash, slug)

    CreateWorktree(base, branch, worktreePath)

    // Save initial state (no plan.md in worktree — plans go to ~/.crit/plans/)
    state := &issueState{
        Slug: slug, Description: cfg.description, Branch: branch,
        Worktree: worktreePath, RepoRoot: repoRoot, Base: base,
        Phase: "setup", OnDone: cfg.onDone,
        PlanPrompt: cfg.planPrompt, ExecPrompt: cfg.execPrompt,
    }
    saveIssueState(state)

    fmt.Fprintf(os.Stderr, "Issue '%s' created\n", slug)
    fmt.Fprintf(os.Stderr, "  Worktree: %s\n", worktreePath)
    fmt.Fprintf(os.Stderr, "  Branch:   %s\n", branch)

    if !cfg.start {
        // Stop here — user must explicitly trigger planning
        fmt.Fprintf(os.Stderr, "\nTo start planning: crit issue --plan %s\n", slug)
        fmt.Fprintf(os.Stderr, "Or open the dashboard:  crit dashboard\n")
        return
    }

    // --start flag: immediately proceed to planning
    runIssueFromPhase(cfg, state)
}
```

### Phase 2a: REFINE (optional)

Invoke `agent_cmd` to expand/refine the issue description before planning. The agent can research the codebase, add technical context, and clarify scope.

```go
func runRefinePhase(state *issueState) error {
    var prompt strings.Builder
    fmt.Fprintf(&prompt, "You are working in: %s\n\n", state.Worktree)
    fmt.Fprintf(&prompt, "Refine and expand the following issue description.\n")
    fmt.Fprintf(&prompt, "Research the codebase to add technical context.\n")
    fmt.Fprintf(&prompt, "Output the refined description (markdown) to stdout.\n\n")
    fmt.Fprintf(&prompt, "## Issue\n%s\n", state.Description)

    state.Phase = "refining"
    saveIssueState(state)

    stdout, err := invokeAgent(state.agentCmd, prompt.String(), state.Worktree)
    if err != nil { return err }

    // Update stored description with agent's refined version
    state.Description = stdout
    state.Phase = "setup"  // back to setup, ready for planning
    saveIssueState(state)
    return nil
}
```

### Phase 2b: PLANNING

Invoke `agent_cmd` with a layered prompt. Agent outputs the plan to stdout, which gets piped into the `crit plan` storage system (just like `echo "plan" | crit plan --name <slug>`).

```go
func runPlanningPhase(state *issueState) error {
    var prompt strings.Builder
    // Layer 1: crit-injected context
    fmt.Fprintf(&prompt, "You are working in: %s\n\n", state.Worktree)
    fmt.Fprintf(&prompt, "Create a detailed implementation plan for the issue below.\n\n")
    fmt.Fprintf(&prompt, "Output the plan as markdown to stdout. Include:\n")
    fmt.Fprintf(&prompt, "- Step-by-step plan\n- Files to create or modify\n- Key design decisions\n- Edge cases\n\n")
    fmt.Fprintf(&prompt, "Do NOT implement anything yet — only plan.\n\n")

    // Layer 2: user's custom planning prompt
    if state.PlanPrompt != "" {
        fmt.Fprintf(&prompt, "Additional instructions:\n%s\n\n", state.PlanPrompt)
    }

    // Layer 3: the (possibly refined) issue description
    fmt.Fprintf(&prompt, "## Issue\n%s\n", state.Description)

    state.Phase = "planning"
    saveIssueState(state)

    stdout, err := invokeAgent(state.agentCmd, prompt.String(), state.Worktree)
    if err != nil { return err }

    // Store plan via existing crit plan infrastructure
    slug := slugify(state.Slug)
    storageDir := planStorageDir(slug)
    savePlanVersion(storageDir, []byte(stdout))

    state.Phase = "plan-review"
    saveIssueState(state)
    return nil
}
```

**Prompt layering order** (all phases):
1. **Crit context** — worktree path, phase-specific instructions, file references
2. **User custom prompt** — `--plan-prompt` / `--exec-prompt`, or from config, or from issue state
3. **Content** — issue description (planning) or approved plan (execution)

`invokeAgent` reuses the same pattern from `server.go`'s agent request handler:
- If command contains `{prompt}`, substitute it
- Otherwise, pipe prompt via stdin
- Set working directory to worktree path
- Stream output to stderr for visibility
- Wait for completion (with configurable timeout, default 10 min)

### Phase 3: PLAN REVIEW

Plan already lives in `~/.crit/plans/<slug>/` from the planning phase. Start a `crit plan` daemon on it.

```go
func runPlanReviewPhase(state *issueState) error {
    slug := slugify(state.Slug)
    storageDir := planStorageDir(slug)
    currentPath := filepath.Join(storageDir, "current.md")

    daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, 0, false, false)
    key := planSessionKey(state.Worktree, slug)

    entry, alive := findAliveSession(key)
    if !alive {
        var err error
        entry, err = startDaemon(key, daemonArgs)
        if err != nil { return err }
    }

    // Feedback loop: review → feedback → agent → review...
    for {
        approved, feedback := runReviewClientRaw(entry)
        if approved {
            killDaemon(entry)
            state.Phase = "executing"  // ready for execution (explicit trigger)
            saveIssueState(state)
            return nil
        }

        // Pipe feedback to agent — agent outputs updated plan to stdout
        stdout, err := invokeAgent(state.agentCmd, feedback, state.Worktree)
        if err != nil { return err }

        // Save new plan version
        savePlanVersion(storageDir, []byte(stdout))

        // Signal round-complete so plan daemon picks up the new version
        http.Post(fmt.Sprintf("http://localhost:%d/api/round-complete", entry.Port), "application/json", nil)
    }
}
```

**Plan review loop:** When the reviewer sends feedback (not approved), we:
1. `runReviewClientRaw` returns the finish prompt (with .crit.json comment summary)
2. Pipe that feedback to agent_cmd — agent outputs updated plan
3. Save new version via `savePlanVersion`
4. Signal round-complete so daemon picks up new `current.md`
5. Loop back to step 1

### Phase 4: EXECUTION

```go
func runExecutionPhase(state *issueState) error {
    planContent, _ := os.ReadFile(filepath.Join(state.Worktree, "plan.md"))

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
    fmt.Fprintf(&prompt, "## Plan\n%s\n", planContent)

    state.Phase = "executing"
    saveIssueState(state)

    return invokeAgent(state.agentCmd, prompt.String(), state.Worktree)
}
```

### Phase 5: CODE REVIEW

Switch to standard `crit` git mode in the worktree. Reuse the existing daemon infrastructure with a cwd override:

```go
func runCodeReviewPhase(cfg issueConfig, worktreePath, branch string) error {
    // Start daemon in worktree (crit auto-detects branch changes via git)
    key := sessionKey(worktreePath, nil)
    entry, _ := startDaemonInDir(key, cfg.daemonArgs(), worktreePath)

    for {
        // Block until reviewer finishes — captures feedback prompt
        approved, feedback := runReviewClientCapture(entry)
        if approved {
            killDaemon(entry)
            return nil
        }

        // Pipe feedback to agent — same flow as `crit` finish → agent stdin
        invokeAgent(cfg.agentCmd, feedback, worktreePath)

        // Signal round-complete so daemon picks up changes
        http.Post(fmt.Sprintf("http://localhost:%d/api/round-complete", entry.Port), ...)
    }
}
```

**Important change needed:** `startDaemon` currently inherits the caller's cwd. We need a variant `startDaemonInDir(key, args, cwd)` that sets the child process working directory. This is a small change to `daemon.go` — add `cmd.Dir = cwd` before `cmd.Start()`.

### `runReviewClientCapture`
A variant of `runReviewClient` that returns the feedback text instead of printing to stdout. The existing `runReviewClientRaw` (used by plan-hook) already returns `(approved, prompt)` — we can reuse it directly.

### Phase 6: COMPLETION

```go
func runCompletionPhase(cfg issueConfig, worktreePath, branch string) error {
    switch cfg.onDone {
    case "pr":
        // Use `gh pr create` from the worktree
        // Title from issue description, body from plan.md
        fmt.Fprintf(os.Stderr, "Creating PR for branch %s...\n", branch)
        cmd := exec.Command("gh", "pr", "create", "--title", cfg.title(), "--body-file", "plan.md")
        cmd.Dir = worktreePath
        cmd.Run()

    case "merge":
        // Merge branch into the original branch
        // git checkout <original> && git merge <branch>

    case "none":
        fmt.Fprintf(os.Stderr, "Worktree: %s\nBranch: %s\n", worktreePath, branch)
        fmt.Fprintln(os.Stderr, "To merge: git merge "+branch)
        fmt.Fprintln(os.Stderr, "To create PR: gh pr create --head "+branch)
    }

    // Always remove worktree (branch persists)
    RemoveWorktree(worktreePath)
    return nil
}
```

## In-Process Mode Switch (Plan → Git)

The daemon stays alive across phases. A new endpoint handles the transition:

### New endpoint: `POST /api/session/switch-mode`

```go
// server.go
func (s *Server) handleSwitchMode(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Mode    string `json:"mode"`    // "git" or "files"
        BaseRef string `json:"base_ref,omitempty"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    // 1. Stop existing file watchers
    s.session.StopWatching()

    // 2. Reinitialize session for new mode
    s.session.SwitchToGitMode(req.BaseRef)
    // - Sets Mode = "git"
    // - Clears plan-specific state (PlanDir, OutputDir override)
    // - Runs ChangedFiles() to build file list from git
    // - Computes diffs for each file
    // - Resets ReviewRound to 1
    // - Clears comments (fresh review)

    // 3. Restart watchers for git mode
    s.session.StartGitWatcher()

    // 4. Broadcast SSE event so frontend reloads
    s.session.Broadcast(SSEEvent{Type: "mode-switched", Data: `{"mode":"git"}`})

    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

### Frontend handling

The frontend listens for the `mode-switched` SSE event and calls `loadSession()` to re-fetch `/api/session`, which now returns git-mode data. The UI naturally transitions — file tree shows changed files, diff view replaces markdown view.

```javascript
// app.js — in SSE handler
case 'mode-switched':
    await loadSession();  // re-fetches /api/session, rebuilds UI
    break;
```

### Session.SwitchToGitMode()

New method on Session (in `session.go`):
```go
func (s *Session) SwitchToGitMode(baseRef string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    s.Mode = "git"
    s.PlanDir = ""
    s.OutputDir = ""  // reset to repo root
    s.ReviewRound = 1
    s.reviewComments = nil

    // Re-detect base ref and changed files
    if baseRef == "" {
        baseRef = s.computeBaseRef()
    }
    s.BaseRef = baseRef
    s.Files = s.detectChangedFiles()
    // Load content + diffs for each file
    for _, f := range s.Files {
        s.loadFileContent(f)
        s.loadFileDiff(f)
    }
}
```

### Why this works well
- Same port, same browser tab — seamless transition
- The orchestrator (`crit issue`) just calls `POST /api/session/switch-mode` after execution phase
- SSE event triggers frontend refresh — no manual reload needed
- Comments from plan review are preserved in `.crit.json` (plan dir) but don't carry into code review

---

## Dashboard (`crit dashboard`)

A separate lightweight daemon that provides a browser UI for managing all active issues.

### CLI
```bash
crit dashboard              # Start dashboard, open browser
crit dashboard --port 3200  # Fixed port
```

### Architecture
- Separate daemon process (like `crit _serve` but for the dashboard)
- Reads `~/.crit/issues/*.json` to list all issues across all projects
- Serves a dedicated frontend page (new `dashboard.html` + `dashboard.js`)
- Session key: `__dashboard` (singleton per machine)

### Dashboard UI
- **Issue list**: table with columns: Project, Description, Branch, Phase, Last Activity
- **Phase indicators**: color-coded badges (Setup, Planning, Plan Review, Executing, Code Review, Done)
- **Full CRUD**:
  - **Create**: "New Issue" form — description, project (repo path), branch name, base branch, on-done strategy, custom plan/execution prompts
  - **Edit**: click into an issue to edit description, prompts, on-done strategy (while in setup/planning phases)
  - **Delete**: remove issue — kills daemon, removes worktree + branch, cleans up state files
- **Phase actions per issue**:
  - "Start Planning" — explicitly triggers the planning phase (agent invocation)
  - "Open Review" — navigates to the issue's crit daemon (plan or code review)
  - "Resume" — reconnects to an interrupted workflow
  - "Skip to Execution" — skip plan review, go straight to execution (for `--no-plan` equivalent)
- **Auto-refresh**: polls `~/.crit/issues/` or uses filesystem watcher

### API Endpoints (dashboard daemon)
- `GET /api/issues` — list all issues with phase/status
- `POST /api/issues` — create new issue (just setup — worktree + state, no agent launch)
- `PUT /api/issues/:slug` — edit issue description, prompts, config
- `DELETE /api/issues/:slug` — abort/cleanup an issue (kill daemon, remove worktree)
- `GET /api/issues/:slug` — detail view, links to crit daemon
- `POST /api/issues/:slug/start` — explicitly trigger next phase (planning or execution)
- `POST /api/issues/:slug/skip` — skip current phase (e.g., skip planning → go to execution)

### Data flow
```
~/.crit/issues/
├── a1b2c3d4-rate-limiting.json      # issue state
├── f5e6d7c8-auth-refactor.json      # issue state
└── ...

Dashboard reads these files, shows status.
Clicking "Open" → checks if daemon alive → redirects to http://localhost:<port>
```

### Settings Screen

The dashboard includes a **Settings** tab that provides a GUI for editing configuration:

**Project settings** (edits `.crit.config.json` in the selected project):
- `agent_cmd` — the agent command
- `plan_prompt` — default planning prompt
- `exec_prompt` — default execution prompt
- `on_done` — default completion strategy (pr / merge / none)
- `base_branch` — default base branch
- `ignore_patterns` — file ignore patterns

**Global settings** (edits `~/.crit.config.json`):
- Same fields as above, but as global defaults

**Per-issue overrides** (edits the issue state file):
- `plan_prompt` — override for this specific issue
- `exec_prompt` — override for this specific issue
- `on_done` — override for this issue
- `branch` — branch name (editable before planning starts)

Settings API:
- `GET /api/settings?project=/path/to/repo` — returns merged config
- `PUT /api/settings/global` — update `~/.crit.config.json`
- `PUT /api/settings/project?project=/path/to/repo` — update `.crit.config.json`
- `PUT /api/issues/:slug/settings` — update issue-specific overrides

### Implementation priority
The dashboard is the **central management point** and should be implemented early — not as a follow-up. Users need it to create/manage issues, configure settings, and trigger phase transitions from a visual UI. The CLI `crit issue` command is the programmatic counterpart.

---

## Key Design Decisions

### Worktree Location
Use `~/.crit/worktrees/<project-hash>/<slug>/` — consistent with the existing `~/.crit/` convention (sessions, plans already live there). The `<project-hash>` is derived from the repo root path to namespace worktrees per-project. This avoids polluting the repo or filesystem and keeps all crit state in one place.

### Agent Invocation
Reuse the same `{prompt}` / stdin pattern from `server.go`. Extract the existing agent invocation logic into a shared `invokeAgent(cmd, prompt, cwd string) (stdout string, err error)` function in a new or existing file, used by both `server.go` (for per-comment agent requests) and `issue.go` (for phase-level orchestration).

### Feedback Loops
Reuse the existing crit review flow as-is. `runReviewClient(entry)` already blocks until the reviewer finishes and prints the feedback prompt to stdout. The orchestrator simply:
1. Captures that stdout output (the finish prompt with comment summary)
2. Pipes it to `agent_cmd` as the next invocation's prompt
3. After the agent completes, signals round-complete to the daemon (or the agent does this by re-running `crit`)
4. Loops back to step 1

This means the feedback loop is literally: `crit review output → agent_cmd stdin → agent edits → crit detects changes → next review`. The exact same flow that works today with `crit` + an agent, just automated by `crit issue` as the outer loop.

### Session Keys
Issue sessions use a distinct key prefix to avoid collisions:
```go
func issueSessionKey(cwd, slug, phase string) string {
    h := sha256.New()
    h.Write([]byte(cwd))
    h.Write([]byte{0})
    h.Write([]byte("__issue:" + slug + ":" + phase))
    return fmt.Sprintf("%x", h.Sum(nil))[:12]
}
```

### State Persistence
Save issue state to `~/.crit/issues/<project-hash>-<slug>.json`:
```json
{
  "slug": "rate-limiting-api",
  "description": "Add rate limiting to API endpoints",
  "branch": "issue/rate-limiting-api",
  "worktree": "~/.crit/worktrees/a1b2c3d4/rate-limiting-api",
  "repo_root": "/path/to/repo",
  "base": "main",
  "phase": "setup",
  "on_done": "pr",
  "plan_prompt": "Focus on performance implications",
  "exec_prompt": "Use table-driven tests for all new code",
  "daemon_port": 0,
  "created_at": "2026-03-28T10:00:00Z",
  "updated_at": "2026-03-28T10:05:00Z"
}
```

This allows:
- **Resuming** interrupted workflows: `crit issue --resume rate-limiting-api`
- **Editing** prompts between phases (via dashboard or direct file edit)
- **Dashboard** reading all issues for display

Phase is updated at each transition so crashes/interrupts can be recovered from. Valid phases: `setup`, `refining`, `planning`, `plan-review`, `executing`, `code-review`, `done`. The `refining` phase only appears if explicitly triggered.

### Daemon CWD Override
Add a `cwd` field to `startDaemon` (or a wrapper `startDaemonInDir`). The `_serve` subprocess `cmd.Dir` is set to the worktree path so git operations see the worktree as the repo.

## Implementation Order

### Phase A: Core infrastructure
1. **`git.go` additions** — `CreateWorktree`, `RemoveWorktree`, `WorktreeList`
2. **Extract `invokeAgent`** — shared helper from server.go's agent dispatch into `agent.go`
3. **`daemon.go` change** — `startDaemonInDir` with cwd override
4. **`config.go` additions** — `on_done`, `plan_prompt`, `exec_prompt` fields

### Phase B: Dashboard (central management point — built early)
5. **`dashboard.go`** — dashboard daemon, issue CRUD API, settings API
6. **`frontend/dashboard.html` + `dashboard.js`** — issue list, create/edit/delete, settings screen
7. **`main.go` dispatch** — add `case "dashboard"`
8. **Issue state management** — `~/.crit/issues/` read/write, `issueState` struct

### Phase C: `crit issue` CLI
9. **`issue.go` scaffolding** — config parsing, `runIssue` entry point, worktree setup
10. **`main.go` dispatch** — add `case "issue"` + help text
11. **Refine phase** (opt-in) — agent refines issue description
12. **Planning phase** — invoke agent, store plan via `crit plan` infra
13. **Plan review phase** — start daemon, feedback loop using `runReviewClientRaw`

### Phase D: Mode switch + execution
14. **`session.go` addition** — `SwitchToGitMode()` method
15. **`server.go` addition** — `POST /api/session/switch-mode` endpoint
16. **`app.js` addition** — handle `mode-switched` SSE event
17. **Execution phase** — invoke agent with approved plan
18. **Code review phase** — feedback loop in git mode
19. **Completion phase** — PR/merge/none handlers

### Phase E: Tests
20. **`issue_test.go`** — unit tests for config, worktree, state persistence
21. **`session_test.go`** — tests for `SwitchToGitMode`
22. **`dashboard_test.go`** — API tests for issue CRUD + settings
23. **Integration test** — mock agent, full lifecycle
24. **E2E** — `mode-switch.spec.ts` for frontend transition, `dashboard.spec.ts`

## Verification

1. **Unit tests**: Config parsing, worktree create/remove, plan seeding, state persistence
2. **Integration test**: Mock agent_cmd (shell script that writes plan.md / modifies a file), run through full lifecycle
3. **Manual test**: `crit issue "Add a hello world endpoint"` with a real agent_cmd, verify:
   - Worktree created with correct branch
   - Agent produces plan.md
   - Plan review opens in browser
   - After approval, agent makes code changes
   - Code review opens in browser showing diff
   - After approval, PR created (or merge/print depending on --on-done)
4. **E2E test**: Could add a `*.issue.spec.ts` project but may be better deferred — the orchestration is mostly Go-side
