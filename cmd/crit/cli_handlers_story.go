package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/story"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// storyFlags holds parsed `crit story` options. Diff-scope flags (--pr,
// --range, default git) are parsed by server.ResolveDaemonCLIConfig and reach
// us via the resolved config, so they are not repeated here.
type storyFlags struct {
	storyFile string // --story-file <path|->
	prep      string // --prep <path>
	skipLLM   bool   // --skip-llm
	clear     bool   // --clear
	refresh   bool   // --refresh
	noSpend   bool   // --no-spend
	guide     bool   // --guide
	noOpen    bool   // --no-open (post-ingest: don't open the browser)
	// scopeArgs are the flags forwarded to the daemon config resolver
	// (everything that isn't a story-only flag).
	scopeArgs []string
}

// storyStartDaemon spawns the review daemon for the post-ingest flow. It is a
// package var so tests can stub the spawn without launching a real process.
var storyStartDaemon = daemon.StartDaemon

// storyPostStory POSTs the story to a running daemon. Package var for tests.
var storyPostStory = postStoryToDaemon

// storyDaemonAlive reports whether a daemon for the session key is running.
// Package var so tests can force the "no daemon" or "daemon present" branch.
var storyDaemonAlive = daemon.FindAliveSession

// storyDaemonHasBrowser reports whether the daemon already has a browser
// client attached (so we don't open a second tab). Package var for tests.
var storyDaemonHasBrowser = daemon.DaemonHasBrowser

// storyValueFlags are story-only flags that take a value.
var storyValueFlags = map[string]func(*storyFlags, string){
	"--story-file": func(f *storyFlags, v string) { f.storyFile = v },
	"--prep":       func(f *storyFlags, v string) { f.prep = v },
}

// storyBoolFlags are story-only boolean flags.
var storyBoolFlags = map[string]func(*storyFlags){
	"--skip-llm": func(f *storyFlags) { f.skipLLM = true },
	"--clear":    func(f *storyFlags) { f.clear = true },
	"--refresh":  func(f *storyFlags) { f.refresh = true },
	"--no-spend": func(f *storyFlags) { f.noSpend = true },
	"--guide":    func(f *storyFlags) { f.guide = true },
	"--no-open":  func(f *storyFlags) { f.noOpen = true },
}

// scopeValueFlags take a value and are forwarded to the daemon config resolver.
var scopeValueFlags = map[string]struct{}{
	"--pr": {}, "--range": {}, "--base-branch": {}, "--scope": {}, "--vcs": {}, "--output": {}, "-o": {},
}

// parseStoryFlags splits `crit story` args into story-only flags and the
// diff-scope args forwarded to the daemon config resolver. It rejects
// positional (non-flag) arguments: story is defined over a diff, not files.
func parseStoryFlags(args []string) (storyFlags, error) {
	var f storyFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case storyBoolFlags[arg] != nil:
			storyBoolFlags[arg](&f)
		case storyValueFlags[arg] != nil:
			v, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return f, err
			}
			storyValueFlags[arg](&f, v)
			i++
		default:
			if err := f.appendScopeArg(args, &i); err != nil {
				return f, err
			}
		}
	}
	return f, nil
}

// appendScopeArg handles a diff-scope flag (forwarded to the resolver) or
// rejects a positional file argument. i is advanced past a consumed value.
func (f *storyFlags) appendScopeArg(args []string, i *int) error {
	arg := args[*i]
	if _, ok := scopeValueFlags[arg]; ok {
		v, err := clicmd.RequireFlagValue(args, *i, arg)
		if err != nil {
			return err
		}
		f.scopeArgs = append(f.scopeArgs, arg, v)
		*i++
		return nil
	}
	// Bare --flags (including --flag=value) are forwarded to the resolver.
	if len(arg) > 2 && arg[:2] == "--" {
		f.scopeArgs = append(f.scopeArgs, arg)
		return nil
	}
	// Anything else is a positional file arg — rejected.
	return clicmd.ExitError{Code: 1, Err: errors.New("story requires a diff (git, --pr, or --range)")}
}

// RunStory implements `crit story`. Phase 1 surface: --story-file, --prep,
// --skip-llm, --clear, --guide (+ --refresh/--no-spend semantics against a
// present story). The default LLM path (exec agent_cmd) is wired in a later
// task.
func runStory(args []string) { clicmd.Exit(runStoryE(args)) }

func runStoryE(args []string) error {
	f, err := parseStoryFlags(args)
	if err != nil {
		return err
	}

	critPath, err := resolveStoryReviewPath(f.scopeArgs)
	if err != nil {
		return err
	}

	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}

	// --clear: drop the story and save. No-op if none present.
	if f.clear {
		cj.Story = nil
		if err := review.SaveCritJSON(critPath, cj); err != nil {
			return clicmd.ExitError{Code: 1, Err: err}
		}
		fmt.Fprintln(os.Stderr, "Cleared story from review.")
		// TODO(story): post-ingest daemon notify (later task) — signal the
		// daemon to re-render in flat file layout.
		return nil
	}

	// --prep: write the prep text + snapshot scope, print path, exit 0.
	if f.prep != "" {
		return runStoryPrep(f)
	}

	// --guide: print the resolved on_story_generate guide, then the JSON
	// schema for the agent-authored fields, to stdout. Exit 0.
	if f.guide {
		return runStoryGuide(f, critPath)
	}

	switch {
	case f.storyFile != "":
		return runStoryIngestFile(f, critPath, cj)
	case f.skipLLM:
		return runStorySkipLLM(f, critPath, cj)
	case f.noSpend:
		// Never call agent_cmd: exit 0 if a story is present, else exit 1.
		if cj.Story != nil {
			fmt.Fprintln(os.Stderr, "Story present.")
			return nil
		}
		return clicmd.ExitError{Code: 1, Err: errors.New("no story and --no-spend set")}
	default:
		return runStoryLLM(f, critPath, cj)
	}
}

// resolveStoryReviewPath resolves the review.json path for the current diff
// scope. The session key is computed identically to `crit review`
// (daemon.SessionKey + session.FocusKeyArgs) so `crit story` and `crit review`
// collide on the same review file for the same scope.
func resolveStoryReviewPath(scopeArgs []string) (string, error) {
	reviewCfg, err := storyReviewConfig(scopeArgs)
	if err != nil {
		return "", err
	}

	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return "", clicmd.ExitError{Code: 1, Err: err}
	}

	// Prefer a running daemon's review path (matches crit review reconnect).
	if path := review.ResolveReviewPathFromDaemon(cwd); path != "" {
		return path, nil
	}

	branch := ""
	if v := vcs.DetectVCS(reviewCfg.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	key := daemon.SessionKey(cwd, branch, session.FocusKeyArgs(reviewCfg))
	path, err := daemon.ReviewFilePath(key)
	if err != nil {
		return "", clicmd.ExitError{Code: 1, Err: err}
	}
	return path, nil
}

// storyReviewConfig resolves the diff-scope config from scope args using the
// same resolver crit review uses, then maps it to the neutral CLIReviewConfig.
func storyReviewConfig(scopeArgs []string) (*session.CLIReviewConfig, error) {
	if session.ResolveServerConfigFn == nil {
		return nil, clicmd.ExitError{Code: 1, Err: errors.New("story: config resolver not wired")}
	}
	sc, err := session.ResolveServerConfigFn(scopeArgs)
	if err != nil {
		return nil, clicmd.ExitError{Code: 1, Err: err}
	}
	if sc == nil {
		// --version early-exit path; treat as a no-op scope.
		return &session.CLIReviewConfig{}, nil
	}
	if len(sc.Files) > 0 {
		return nil, clicmd.ExitError{Code: 1, Err: errors.New("story requires a diff (git, --pr, or --range)")}
	}
	return sc, nil
}

// buildStoryScope constructs the session for the current scope and returns its
// neutral StoryScope snapshot (base/head SHA, commits, indexed + ignored hunks).
func buildStoryScope(scopeArgs []string) (session.StoryScope, error) {
	dsc, err := server.ResolveDaemonCLIConfig(scopeArgs)
	if err != nil {
		return session.StoryScope{}, clicmd.ExitError{Code: 1, Err: err}
	}
	if dsc == nil {
		return session.StoryScope{}, clicmd.ExitError{Code: 1, Err: errors.New("story: could not resolve diff scope")}
	}
	sess, err := server.CreateSession(dsc)
	if err != nil {
		return session.StoryScope{}, clicmd.ExitError{Code: 1, Err: err}
	}
	server.ApplySessionOverrides(sess, dsc)
	return sess.StoryScope(dsc.IgnorePatterns), nil
}

func runStoryPrep(f storyFlags) error {
	scope, err := buildStoryScope(f.scopeArgs)
	if err != nil {
		return err
	}
	in, _, _ := story.FromScope(scope)
	prep := story.BuildPrep(in)
	if err := os.WriteFile(f.prep, []byte(prep.Text), 0o644); err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	fmt.Println(f.prep)
	return nil
}

// storySchemaJSON is the JSON shape the agent must emit: only prologue,
// chapters, and support (crit fills version/generated_at/base_sha/head_sha/
// scope_fingerprint/coverage after ingest — see internal/session.Story).
const storySchemaJSON = `{
  "prologue": {
    "summary": "string, 1-3 sentences, stands alone",
    "motivation": "string, optional",
    "diagram": "string, optional Mermaid diagram, default \"\"",
    "focus_areas": [
      {"area": "string", "severity": "string, optional"}
    ],
    "complexity": "one of: low, medium, high"
  },
  "chapters": [
    {
      "id": "string, e.g. \"ch1\"",
      "title": "string, <=24 chars recommended",
      "summary": "string, one-liner, must stand alone",
      "hunk_refs": [
        {"file_path": "string", "old_start": "int, 0 for new files"}
      ],
      "diagram": "string, optional Mermaid diagram, default \"\""
    }
  ],
  "support": [
    {
      "hunk_refs": [
        {"file_path": "string", "old_start": "int, 0 for new files"}
      ],
      "reason": "string, e.g. \"Lockfile churn.\""
    }
  ]
}`

// resolveStoryGuide resolves the on_story_generate guide through the same
// 5-level prompt-override precedence as other hooks, interpolating the §4.4
// StoryContext variables (prep file PATH, schema, SHAs, PR vars, session key,
// review path). It fires the SAME project-prompt trust gate the --guide path
// uses, so the LLM path and --guide never diverge on trust. prepPath is the
// on-disk prep file the guide instructs the agent to READ; sessionKey is the
// review session key (empty for --guide, which has no scope resolution).
func resolveStoryGuide(scope session.StoryScope, critPath, prepPath, sessionKey string) (string, error) {
	projectDir, err := daemon.ResolvedCWD()
	if err != nil {
		return "", clicmd.ExitError{Code: 1, Err: err}
	}
	homeDir, _ := os.UserHomeDir()

	globalPrompts, projectPrompts := config.LoadPromptMaps(projectDir)
	trust, err := prompt.EvaluateTrust(projectDir, projectPrompts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: evaluating project prompt trust: %v\n", err)
	}
	if trust.Untrusted {
		return "", clicmd.ExitError{Code: 1, Err: errors.New("project prompts are not trusted yet; run crit and choose a trust option, or use --story-file/--skip-llm")}
	}

	diffScopeKind := "workingTree"
	if scope.HeadSHA != "" {
		diffScopeKind = "committed"
	}

	ctx := prompt.StoryContext{
		PrepPath:        prepPath,
		StorySchemaJSON: storySchemaJSON,
		CommitMessages:  strings.Join(scope.CommitMessages, "\n"),
		DiffScopeKind:   diffScopeKind,
		BaseSHA:         scope.BaseSHA,
		HeadSHA:         scope.HeadSHA,
		MergeBaseSHA:    scope.MergeBaseSHA,
		SessionKey:      sessionKey,
		ReviewPath:      critPath,
	}
	if scope.PRNumber > 0 {
		ctx.PRNumber = strconv.Itoa(scope.PRNumber)
	}
	ctx.PRURL = scope.PRURL
	// TODO(story): PRTitle/PRBody require a `gh pr view` call that the story
	// scope doesn't currently make; the template variables exist but stay
	// empty until PR metadata is threaded through the daemon config resolver.
	if ctx.PrepPath == "" {
		ctx.PrepPath = "<run `crit story --prep <path>` first, then pass that path here>"
	}

	result, err := prompt.RenderHook(globalPrompts, projectPrompts, projectDir, homeDir, trust.UseProject, prompt.HookStoryGenerate, ctx.TemplateData())
	if err != nil {
		return "", clicmd.ExitError{Code: 1, Err: err}
	}
	return result.Text, nil
}

// runStoryGuide prints the resolved on_story_generate guide followed by
// "\n\n---\n\n" and the JSON schema in a fenced code block, then exits 0
// (spec §11 decision 1).
func runStoryGuide(f storyFlags, critPath string) error {
	scope, err := buildStoryScope(f.scopeArgs)
	if err != nil {
		return err
	}

	guide, err := resolveStoryGuide(scope, critPath, f.prep, "")
	if err != nil {
		return err
	}

	fmt.Println(strings.TrimRight(guide, "\n"))
	fmt.Println()
	fmt.Println("---")
	fmt.Println()
	fmt.Println("```json")
	fmt.Println(storySchemaJSON)
	fmt.Println("```")
	return nil
}

// runStoryIngestFile reads a pre-authored story JSON, ingests it against the
// live diff, and saves on success. The coverage report is printed to stdout as
// JSON on every ingest (success or failure).
func runStoryIngestFile(f storyFlags, critPath string, cj review.CritJSON) error {
	if cj.Story != nil && !f.refresh {
		fmt.Fprintln(os.Stderr, "story already present (use --refresh to regenerate)")
		return nil
	}

	raw, err := readStoryFile(f.storyFile)
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	var st session.Story
	if err := json.Unmarshal(raw, &st); err != nil {
		return clicmd.ExitError{Code: 1, Err: fmt.Errorf("parsing story JSON: %w", err)}
	}

	scope, err := buildStoryScope(f.scopeArgs)
	if err != nil {
		return err
	}
	_, indexed, ignored := story.FromScope(scope)

	res, ingestErr := story.Run(story.Ingest{
		Story:           &st,
		Indexed:         indexed,
		Ignored:         ignored,
		LiveFingerprint: story.Fingerprint(indexed),
	})
	printCoverage(res.Coverage)
	if ingestErr != nil {
		return clicmd.ExitError{Code: 1, Err: ingestErr}
	}

	return saveStory(f, critPath, cj, &st)
}

// runStorySkipLLM writes a stub story with all hunks in a single support entry
// (reason "stub"). Used to exercise the renderer / E2E without an LLM.
func runStorySkipLLM(f storyFlags, critPath string, cj review.CritJSON) error {
	if cj.Story != nil && !f.refresh {
		fmt.Fprintln(os.Stderr, "story already present (use --refresh or --clear then re-run)")
		return nil
	}
	scope, err := buildStoryScope(f.scopeArgs)
	if err != nil {
		return err
	}
	prepIn, _, _ := story.FromScope(scope)
	prep := story.BuildPrep(prepIn)

	var refs []session.StoryHunkRef
	for _, h := range prep.Indexed {
		refs = append(refs, session.StoryHunkRef{FilePath: h.FilePath, OldStart: h.OldStart})
	}
	st := session.Story{
		Version:          1,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		BaseSHA:          prep.BaseSHA,
		HeadSHA:          prep.HeadSHA,
		ScopeFingerprint: prep.ScopeFingerprint,
		Chapters:         []session.StoryChapter{},
		Support: []session.StorySupportEntry{
			{HunkRefs: refs, Reason: story.ReasonStub},
		},
		Coverage: &session.StoryCoverage{
			OK:      true,
			Indexed: len(refs),
			Placed:  len(refs),
		},
	}
	printCoverage(st.Coverage)
	return saveStory(f, critPath, cj, &st)
}

// runStoryLLM is the default path: exec agent_cmd with a prompt-by-reference
// prompt (the guide points the agent at the on-disk prep file), extract the
// JSON per §4.3 (fence-strip / brace-substring, strict parse, one retry with
// the parse error fed back), ingest against the live diff, save, then run the
// post-ingest flow.
func runStoryLLM(f storyFlags, critPath string, cj review.CritJSON) error {
	if cj.Story != nil && !f.refresh {
		fmt.Fprintln(os.Stderr, "story already present (use --refresh to regenerate)")
		return nil
	}

	agentCmd := config.LoadConfig(mustCWD()).AgentCmd
	if strings.TrimSpace(agentCmd) == "" {
		return clicmd.ExitError{Code: 1, Err: errors.New("no agent_cmd configured; set agent_cmd in ~/.crit.config.json, or use --story-file/--skip-llm/--guide")}
	}

	scope, err := buildStoryScope(f.scopeArgs)
	if err != nil {
		return err
	}

	// Write the full, untrimmed prep to a temp file the agent reads (§4.3).
	prepIn, _, _ := story.FromScope(scope)
	prep := story.BuildPrep(prepIn)
	prepFile, err := os.CreateTemp("", "crit-story-prep-*.txt")
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	prepPath := prepFile.Name()
	defer os.Remove(prepPath)
	if _, err := prepFile.WriteString(prep.Text); err != nil {
		prepFile.Close()
		return clicmd.ExitError{Code: 1, Err: err}
	}
	prepFile.Close()

	guide, err := resolveStoryGuide(scope, critPath, prepPath, resolveStorySessionKey(f.scopeArgs))
	if err != nil {
		return err
	}

	st, err := generateStory(agentCmd, guide)
	if err != nil {
		return err
	}

	// Ingest against the live diff scope (same validation as --story-file).
	_, indexed, ignored := story.FromScope(scope)
	res, ingestErr := story.Run(story.Ingest{
		Story:           st,
		Indexed:         indexed,
		Ignored:         ignored,
		LiveFingerprint: story.Fingerprint(indexed),
	})
	printCoverage(res.Coverage)
	if ingestErr != nil {
		return clicmd.ExitError{Code: 1, Err: ingestErr}
	}

	return saveStory(f, critPath, cj, st)
}

// generateStory execs agent_cmd with the story prompt, extracts+parses the JSON
// per §4.3, and retries EXACTLY ONCE on a parse failure with the parse error
// appended. On a second parse failure it saves the raw stdout to a temp file
// and returns an error naming that file (exit 1).
func generateStory(agentCmd, guide string) (*session.Story, error) {
	firstPrompt := story.BuildStoryPrompt(guide, storySchemaJSON, "")

	out, err := execAgentCmd(agentCmd, firstPrompt)
	if err != nil {
		return nil, clicmd.ExitError{Code: 1, Err: fmt.Errorf("agent_cmd failed: %w", err)}
	}
	st, parseErr := parseStoryJSON(out)
	if parseErr == nil {
		return st, nil
	}

	// One retry with the parse error fed back (§4.3).
	fmt.Fprintf(os.Stderr, "agent output was not valid story JSON (%v); retrying once...\n", parseErr)
	retryPrompt := story.BuildStoryPrompt(guide, storySchemaJSON, story.RetryFeedback(parseErr))
	retryOut, err := execAgentCmd(agentCmd, retryPrompt)
	if err != nil {
		return nil, clicmd.ExitError{Code: 1, Err: fmt.Errorf("agent_cmd failed on retry: %w", err)}
	}
	st, parseErr = parseStoryJSON(retryOut)
	if parseErr == nil {
		return st, nil
	}

	rawPath := saveRawAgentOutput(retryOut)
	return nil, clicmd.ExitError{Code: 1, Err: fmt.Errorf("agent output was not valid story JSON after one retry (raw output saved to %s): %w", rawPath, parseErr)}
}

// parseStoryJSON extracts the JSON candidate from agent stdout and unmarshals
// it into a Story. "Strict" here (§4.3) means the candidate must be valid JSON
// that unmarshals into the Story shape — extra keys the agent volunteers (e.g.
// "agent") are ignored, matching the --story-file ingest path.
func parseStoryJSON(out string) (*session.Story, error) {
	candidate := story.ExtractJSON(out)
	if strings.TrimSpace(candidate) == "" {
		return nil, errors.New("empty agent output")
	}
	var st session.Story
	if err := json.Unmarshal([]byte(candidate), &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// execAgentCmd runs agent_cmd exactly like the server's runAgentCmd
// (internal/server/server.go): split on whitespace, replace a {prompt}
// placeholder with the prompt as a single arg, else pipe the prompt on stdin;
// cmd.Dir = repo root. A 5-minute deadline bounds each attempt (§10).
func execAgentCmd(agentCmd, promptText string) (string, error) {
	parts := strings.Fields(agentCmd)
	if len(parts) == 0 {
		return "", errors.New("agent_cmd is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	hasPlaceholder := false
	for i, p := range parts {
		if p == "{prompt}" {
			parts[i] = promptText
			hasPlaceholder = true
		}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	if !hasPlaceholder {
		cmd.Stdin = strings.NewReader(promptText)
	}
	repoRoot, err := vcs.RepoRoot()
	if err == nil {
		cmd.Dir = repoRoot
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timed out after 5m: %w", err)
		}
		return "", fmt.Errorf("%w\nstderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// saveRawAgentOutput writes the agent's raw stdout to a temp file so the user
// can inspect what the agent actually produced after a parse failure. Returns
// the path (or a placeholder note if writing fails).
func saveRawAgentOutput(out string) string {
	tmp, err := os.CreateTemp("", "crit-story-agent-output-*.txt")
	if err != nil {
		return "(could not write temp file)"
	}
	defer tmp.Close()
	_, _ = tmp.WriteString(out)
	return tmp.Name()
}

// mustCWD resolves the working directory, falling back to "." so config lookup
// degrades gracefully rather than panicking.
func mustCWD() string {
	if cwd, err := daemon.ResolvedCWD(); err == nil {
		return cwd
	}
	return "."
}

// resolveStorySessionKey computes the review session key for the scope so the
// prompt can carry {{.session_key}}. Best-effort: returns "" on any failure.
func resolveStorySessionKey(scopeArgs []string) string {
	reviewCfg, err := storyReviewConfig(scopeArgs)
	if err != nil {
		return ""
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return ""
	}
	branch := ""
	if v := vcs.DetectVCS(reviewCfg.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	return daemon.SessionKey(cwd, branch, session.FocusKeyArgs(reviewCfg))
}

// saveStory sets the story on the review JSON, persists it via SaveCritJSON,
// then runs the post-ingest flow (§4.1): notify a running daemon so the open
// review re-renders, or spawn the daemon detached and open the browser. It
// never blocks — the post-ingest step is best-effort and returns exit 0.
func saveStory(f storyFlags, critPath string, cj review.CritJSON, st *session.Story) error {
	cj.Story = st
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	fmt.Fprintln(os.Stderr, "Story saved.")
	postIngest(f, st)
	return nil
}

// postIngest connects the freshly-saved story to a review surface (§4.1). If a
// daemon for this session key is already running, it POSTs the story so the
// open page live-updates via the story-updated SSE event. Otherwise it spawns
// the daemon detached (same path as first-run `crit`) and opens the browser,
// respecting --no-open / config. Any failure is logged, not fatal: the story
// is already on disk, so the next `crit story`/`crit` will pick it up.
func postIngest(f storyFlags, st *session.Story) {
	reviewCfg, err := storyReviewConfig(f.scopeArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not resolve session for the review UI: %v\n", err)
		return
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not resolve cwd for the review UI: %v\n", err)
		return
	}
	branch := ""
	if v := vcs.DetectVCS(reviewCfg.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	key := daemon.SessionKey(cwd, branch, session.FocusKeyArgs(reviewCfg))

	if entry, alive := storyDaemonAlive(key); alive {
		if err := storyPostStory(entry, st); err != nil {
			fmt.Fprintf(os.Stderr, "note: could not notify the running review daemon: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "Updated the open review at %s\n", entry.BaseURL())
		return
	}

	// No daemon: spawn one detached (same args flow as `crit review`) and open
	// the browser. crit review's args are the scope args; --no-open is honored
	// via config + flag.
	entry, err := storyStartDaemon(key, storyDaemonArgs(f.scopeArgs))
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not start the review daemon: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "Started crit daemon at %s (session %s, PID %d)\n", entry.BaseURL(), key, entry.PID)
	if !storyNoOpen(f, cwd) && !storyDaemonHasBrowser(entry) {
		openBrowser(entry.BaseURL(), config.LoadConfig(cwd).OpenCmd)
	}
}

// postStoryToDaemon POSTs the story to a running daemon's /api/story endpoint
// (body shape matches handleStoryPost: {"story": ...}). /api/story is
// withReady-gated (503 until session init completes), and FindAliveSession
// only probes the ungated /api/health, so "alive" does not imply "ready". It
// therefore polls /api/session until it stops returning 503 first — the
// canonical readiness loop from daemon.RunReviewClient (waitForDaemonReady).
func postStoryToDaemon(entry daemon.SessionEntry, st *session.Story) error {
	base := entry.ConnURL()
	if err := waitDaemonReady(base); err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{"story": st})
	if err != nil {
		return err
	}
	resp, err := http.Post(base+"/api/story", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("daemon returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// waitDaemonReady polls GET base+"/api/session" until it stops returning 503
// (session init done) or a bounded deadline elapses. Mirrors the canonical
// readiness loop in daemon.RunReviewClient. base is a ConnURL (scheme+host+port).
func waitDaemonReady(base string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(base + "/api/session")
		if err != nil {
			return fmt.Errorf("could not reach daemon: %w", err)
		}
		status := resp.StatusCode
		resp.Body.Close()
		if status != http.StatusServiceUnavailable {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("daemon did not become ready within 30s")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// storyDaemonArgs strips story-only flags from the scope args so the spawned
// daemon runs a plain review over the same diff scope. The scope args are
// already free of story-only flags (parseStoryFlags separates them), so this
// is currently a passthrough — kept as a seam in case the daemon needs a
// dedicated story flag later.
func storyDaemonArgs(scopeArgs []string) []string {
	return scopeArgs
}

// storyNoOpen reports whether the browser must NOT be opened: the CLI/config
// no_open setting. Mirrors how crit review resolves NoOpen from config.
func storyNoOpen(f storyFlags, cwd string) bool {
	if f.noOpen {
		return true
	}
	return config.LoadConfig(cwd).NoOpen
}

func printCoverage(c *session.StoryCoverage) {
	if c == nil {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	fmt.Println(string(b))
}

func readStoryFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
