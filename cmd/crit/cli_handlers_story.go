package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
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
	// scopeArgs are the flags forwarded to the daemon config resolver
	// (everything that isn't a story-only flag).
	scopeArgs []string
}

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
// --skip-llm, --clear (+ --refresh/--no-spend semantics against a present
// story). The default LLM path and --guide are wired in later tasks.
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

	// --guide and the default LLM path are not wired in this task.
	if f.guide {
		return clicmd.ExitError{Code: 1, Err: errors.New("story --guide is not wired yet (coming in this branch); use --story-file or --skip-llm")}
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
		// Default LLM path (exec agent_cmd) lands in a later task.
		return clicmd.ExitError{Code: 1, Err: errors.New("story generation via agent_cmd is not wired yet (coming in this branch); use --story-file or --skip-llm")}
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

	return saveStory(critPath, cj, &st)
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
	return saveStory(critPath, cj, &st)
}

// saveStory sets the story on the review JSON and persists it via SaveCritJSON.
func saveStory(critPath string, cj review.CritJSON, st *session.Story) error {
	cj.Story = st
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	fmt.Fprintln(os.Stderr, "Story saved.")
	// TODO(story): post-ingest daemon notify (later task) — POST /api/story to a
	// running daemon, or spawn the daemon detached + open the browser.
	return nil
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
