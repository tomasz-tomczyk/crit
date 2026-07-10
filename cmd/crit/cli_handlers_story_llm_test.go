package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// sessionStoryStub returns a distinctive existing story (Version 42) so tests
// can tell a preserved story from a regenerated one.
func sessionStoryStub() *session.Story { return &session.Story{Version: 42} }

// stubPostIngest replaces the daemon-spawn + browser-open seams with no-ops so
// LLM-path tests exercise generation/ingest/save without launching a daemon.
// It restores the originals via t.Cleanup.
func stubPostIngest(t *testing.T) {
	t.Helper()
	origAlive := storyDaemonAlive
	origStart := storyStartDaemon
	origPost := storyPostStory
	origOpen := openBrowser
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
	storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) {
		return daemon.SessionEntry{Port: 0, PID: 0}, nil
	}
	storyPostStory = func(daemon.SessionEntry, *session.Story) error { return nil }
	openBrowser = func(string, string) {}
	t.Cleanup(func() {
		storyDaemonAlive = origAlive
		storyStartDaemon = origStart
		storyPostStory = origPost
		openBrowser = origOpen
	})
}

// setupStoryRepoLLM sets up the story repo, then re-points HOME at a fresh
// temp dir OUTSIDE the repo so config files and review storage don't land in
// the repo's diff scope (setupStoryRepo sets HOME=repo, which would). cwd
// stays the repo. Returns the repo dir and a scratch dir for agent scripts.
func setupStoryRepoLLM(t *testing.T) (repoDir, scratchDir string) {
	t.Helper()
	repoDir, _, _ = setupStoryRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	return repoDir, t.TempDir()
}

// setAgentCmd writes agent_cmd into the isolated global config for the test.
func setAgentCmd(t *testing.T, cmd string) {
	t.Helper()
	path := config.GlobalConfigPath()
	body, _ := json.Marshal(map[string]any{"agent_cmd": cmd})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("writing global config: %v", err)
	}
}

// fakeAgentScript writes an executable shell script (in dir, which must be
// OUTSIDE the repo) that drains stdin and prints out to stdout. Returns the
// script path (usable as agent_cmd).
func fakeAgentScript(t *testing.T, dir, out string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-agent shell script not portable to Windows")
	}
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncat >/dev/null 2>&1 || true\ncat <<'CRIT_STORY_EOF'\n" + out + "\nCRIT_STORY_EOF\n"
	path := filepath.Join(dir, "fake-agent.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake agent: %v", err)
	}
	return path
}

// validCannedStory is a story JSON covering setupStoryRepo's single app.go hunk
// (new file => old_start 0).
const validCannedStory = `{
  "version": 1,
  "agent": "fake-agent",
  "prologue": {
    "title": "Canned story",
    "overview": "A canned story for tests.",
    "key_changes": ["Covers the fixture hunk."],
    "risks": ["Fixture coverage depends on the app.go new-file hunk."]
  },
  "chapters": [
    {"id": "ch1", "title": "Canned", "summary": "Covers the fixture hunk.",
     "hunk_refs": [{"file_path": "app.go", "old_start": 0}]}
  ]
}`

func TestStoryLLM_HappyPath(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, validCannedStory)
	setAgentCmd(t, agent)

	if err := runStoryE(nil); err != nil {
		t.Fatalf("expected clean generation, got: %v", err)
	}

	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil {
		t.Fatal("story not persisted after LLM generation")
	}
	if len(cj.Story.Chapters) != 1 || cj.Story.Chapters[0].Title != "Canned" {
		t.Fatalf("unexpected persisted story: %+v", cj.Story)
	}
	if cj.Story.Coverage == nil || !cj.Story.Coverage.OK {
		t.Fatalf("expected clean coverage, got %+v", cj.Story.Coverage)
	}
}

func TestStoryLLM_PrintsProgressWhileGenerating(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, validCannedStory)
	setAgentCmd(t, agent)

	var stderr strings.Builder
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, r)
		close(done)
	}()

	err := runStoryE(nil)
	w.Close()
	<-done
	os.Stderr = old

	if err != nil {
		t.Fatalf("expected clean generation, got: %v", err)
	}
	out := stderr.String()
	for _, want := range []string{"Generating story", "please wait", "asking agent_cmd"} {
		if !strings.Contains(out, want) {
			t.Fatalf("story generation stderr missing %q:\n%s", want, out)
		}
	}
}

func TestStoryLLM_FencedOutput(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	fenced := "```json\n" + validCannedStory + "\n```"
	agent := fakeAgentScript(t, scratch, fenced)
	setAgentCmd(t, agent)

	if err := runStoryE(nil); err != nil {
		t.Fatalf("fenced output should be extracted and ingested, got: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil {
		t.Fatal("story not persisted from fenced output")
	}
}

func TestStoryLLM_ProseWrappedOutput(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	prose := "Sure, here is the story you asked for:\n\n" + validCannedStory + "\n\nLet me know if you'd like changes."
	agent := fakeAgentScript(t, scratch, prose)
	setAgentCmd(t, agent)

	if err := runStoryE(nil); err != nil {
		t.Fatalf("prose-wrapped output should be extracted, got: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil {
		t.Fatal("story not persisted from prose-wrapped output")
	}
}

func TestStoryLLM_InvalidTwiceExitsOneAndSavesRaw(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, "not json at all, sorry")
	setAgentCmd(t, agent)

	err := runStoryE(nil)
	if err == nil {
		t.Fatal("expected exit 1 after two invalid outputs")
	}
	// The error must name the raw-output temp file.
	if !strings.Contains(err.Error(), "raw output saved to") {
		t.Fatalf("error should name the saved raw output file, got: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story != nil {
		t.Fatal("no story must be persisted after parse failure")
	}
}

// TestStoryLLM_RetryAppendsError verifies the retry: an agent that emits prose
// on the first call and valid JSON on the second (tracked via a state file)
// succeeds, proving exactly one retry happened with the retry prompt.
func TestStoryLLM_RetryAppendsError(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	if runtime.GOOS == "windows" {
		t.Skip("shell script not portable to Windows")
	}

	stateFile := filepath.Join(t.TempDir(), "calls")
	// First invocation: no state file yet => emit garbage, create the file.
	// Second invocation: state file exists => emit valid JSON.
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"cat >/dev/null 2>&1 || true\n" +
		"if [[ -f '" + stateFile + "' ]]; then\n" +
		"  cat <<'CRIT_STORY_EOF'\n" + validCannedStory + "\nCRIT_STORY_EOF\n" +
		"else\n" +
		"  touch '" + stateFile + "'\n" +
		"  echo 'here you go, no json'\n" +
		"fi\n"
	agent := filepath.Join(scratch, "retry-agent.sh")
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	setAgentCmd(t, agent)

	if err := runStoryE(nil); err != nil {
		t.Fatalf("expected success after one retry, got: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil {
		t.Fatal("story not persisted after successful retry")
	}
}

func TestStoryLLM_StoryPresentWithoutRefresh(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)

	// Sentinel agent: writes a marker file if ever invoked. Story-present resume
	// must NOT exec agent_cmd, so the marker must be absent afterwards.
	marker := filepath.Join(t.TempDir(), "agent-ran")
	agent := writeSentinelAgent(t, scratch, marker)
	setAgentCmd(t, agent)

	// Capture the resume flow's browser open + spawn seams (no daemon running).
	origAlive := storyDaemonAlive
	origStart := storyStartDaemon
	origOpen := openBrowser
	origHasBrowser := storyDaemonHasBrowser
	t.Cleanup(func() {
		storyDaemonAlive = origAlive
		storyStartDaemon = origStart
		openBrowser = origOpen
		storyDaemonHasBrowser = origHasBrowser
	})
	var openedURL string
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
	storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) {
		return daemon.SessionEntry{Port: 5555}, nil
	}
	storyDaemonHasBrowser = func(daemon.SessionEntry) bool { return false }
	openBrowser = func(u, _ string) { openedURL = u }

	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	cj.Story = sessionStoryStub()
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}

	// Without --refresh: resume (exit 0), agent untouched, story untouched.
	if err := runStoryE(nil); err != nil {
		t.Fatalf("story-present resume should exit 0, got: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("agent_cmd must NOT be invoked when a story is already present")
	}
	reloaded, _ := review.LoadCritJSON(critPath)
	if reloaded.Story == nil || reloaded.Story.Version != 42 {
		t.Fatal("existing story must be left untouched without --refresh")
	}
	if !strings.HasSuffix(openedURL, "#story") {
		t.Fatalf("resume must open the browser at the story view (#story), got %q", openedURL)
	}
}

// writeSentinelAgent writes an executable agent script that touches markerPath
// when invoked (so tests can assert agent_cmd was NOT called). dir must be
// OUTSIDE the repo. Returns the script path (usable as agent_cmd).
func writeSentinelAgent(t *testing.T, dir, markerPath string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sentinel shell script not portable to Windows")
	}
	script := "#!/usr/bin/env bash\ntouch " + strconv.Quote(markerPath) + "\n"
	path := filepath.Join(dir, "sentinel-agent.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing sentinel agent: %v", err)
	}
	return path
}

// TestStoryResume_OpensBrowserAtStoryView covers the two resume branches: an
// already-running daemon (open a tab at #story, no spawn) and no daemon (spawn
// + open at #story), plus --no-open suppression in both.
func TestStoryResume_OpensBrowserAtStoryView(t *testing.T) {
	setupStoryRepoLLM(t)

	origAlive := storyDaemonAlive
	origStart := storyStartDaemon
	origPost := storyPostStory
	origOpen := openBrowser
	origHasBrowser := storyDaemonHasBrowser
	t.Cleanup(func() {
		storyDaemonAlive = origAlive
		storyStartDaemon = origStart
		storyPostStory = origPost
		openBrowser = origOpen
		storyDaemonHasBrowser = origHasBrowser
	})
	storyDaemonHasBrowser = func(daemon.SessionEntry) bool { return false }
	storyPostStory = func(daemon.SessionEntry, *session.Story) error {
		t.Error("resume must NOT re-POST the story to the daemon")
		return nil
	}

	t.Run("running daemon opens tab at #story without spawning", func(t *testing.T) {
		var spawned bool
		var openedURL string
		storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
			return daemon.SessionEntry{Port: 4321}, true
		}
		storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) { spawned = true; return daemon.SessionEntry{}, nil }
		openBrowser = func(u, _ string) { openedURL = u }

		if err := resumeStory(storyFlags{}); err != nil {
			t.Fatalf("resume returned error: %v", err)
		}
		if spawned {
			t.Error("must NOT spawn a daemon when one is already running")
		}
		if !strings.HasSuffix(openedURL, "#story") {
			t.Fatalf("expected browser open at #story, got %q", openedURL)
		}
	})

	t.Run("no daemon spawns and opens at #story", func(t *testing.T) {
		var spawned bool
		var openedURL string
		storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
		storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) {
			spawned = true
			return daemon.SessionEntry{Port: 4322}, nil
		}
		openBrowser = func(u, _ string) { openedURL = u }

		if err := resumeStory(storyFlags{}); err != nil {
			t.Fatalf("resume returned error: %v", err)
		}
		if !spawned {
			t.Error("expected a daemon spawn when none is running")
		}
		if !strings.HasSuffix(openedURL, "#story") {
			t.Fatalf("expected browser open at #story, got %q", openedURL)
		}
	})

	t.Run("--no-open suppresses the browser on resume", func(t *testing.T) {
		var opened bool
		storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
			return daemon.SessionEntry{Port: 4323}, true
		}
		openBrowser = func(string, string) { opened = true }
		if err := resumeStory(storyFlags{noOpen: true}); err != nil {
			t.Fatalf("resume returned error: %v", err)
		}
		if opened {
			t.Error("--no-open must suppress the browser on resume (running daemon)")
		}

		opened = false
		storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
		storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) {
			return daemon.SessionEntry{Port: 4324}, nil
		}
		if err := resumeStory(storyFlags{noOpen: true}); err != nil {
			t.Fatalf("resume returned error: %v", err)
		}
		if opened {
			t.Error("--no-open must suppress the browser on resume (spawn path)")
		}
	})
}

func TestStoryLLM_RefreshRegenerates(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, validCannedStory)
	setAgentCmd(t, agent)

	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	cj.Story = sessionStoryStub()
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}

	if err := runStoryE([]string{"--refresh"}); err != nil {
		t.Fatalf("--refresh should regenerate, got: %v", err)
	}
	reloaded, _ := review.LoadCritJSON(critPath)
	if reloaded.Story == nil {
		t.Fatal("expected regenerated story")
	}
	if len(reloaded.Story.Chapters) != 1 || reloaded.Story.Chapters[0].Title != "Canned" {
		t.Fatalf("story was not regenerated: %+v", reloaded.Story)
	}
}

func TestStoryLLM_NoAgentCmdConfigured(t *testing.T) {
	setupStoryRepo(t)
	stubPostIngest(t)
	// No agent_cmd written -> LLM path must exit 1 with a clear message.
	err := runStoryE(nil)
	if err == nil {
		t.Fatal("expected exit 1 when no agent_cmd configured")
	}
	if !strings.Contains(err.Error(), "agent_cmd") {
		t.Fatalf("error should mention agent_cmd, got: %v", err)
	}
}

// TestStoryLLM_NoSpendNeverExecs verifies --no-spend never touches agent_cmd:
// even with a garbage agent, --no-spend with no story exits 1 (no exec) and
// with a story exits 0.
func TestStoryLLM_NoSpendNeverExecs(t *testing.T) {
	_, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, "garbage that would fail to parse")
	setAgentCmd(t, agent)

	if err := runStoryE([]string{"--no-spend"}); err == nil {
		t.Fatal("--no-spend with no story must exit 1")
	}

	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	cj.Story = sessionStoryStub()
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	if err := runStoryE([]string{"--no-spend"}); err != nil {
		t.Fatalf("--no-spend with story present must exit 0, got: %v", err)
	}
}

func TestStoryLLM_UntrustedProjectPromptBlocks(t *testing.T) {
	repo, scratch := setupStoryRepoLLM(t)
	stubPostIngest(t)
	agent := fakeAgentScript(t, scratch, validCannedStory)
	setAgentCmd(t, agent)

	// A project-level on_story_generate override in an untrusted project must
	// block the LLM path with the same gate --guide uses.
	promptDir := filepath.Join(repo, ".crit", "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "on_story_generate.md"), []byte("EVIL PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStoryE(nil)
	if err == nil {
		t.Fatal("untrusted project prompt override must block the LLM path")
	}
	if !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("error should mention trust, got: %v", err)
	}
}

// mustHomeAgent guards against a stale HOME leaking a developer's agent_cmd.
func TestStoryLLM_HomeIsolated(t *testing.T) {
	setupStoryRepo(t)
	if got := config.GlobalConfigPath(); !strings.HasPrefix(got, os.Getenv("HOME")) {
		t.Fatalf("global config path %q not under test HOME %q", got, os.Getenv("HOME"))
	}
}

// TestPostStoryToDaemon_BodyShape verifies postStoryToDaemon sends the exact
// {"story": ...} body that the daemon's handleStoryPost expects, to /api/story.
func TestPostStoryToDaemon_BodyShape(t *testing.T) {
	var gotPath string
	var gotBody struct {
		Story *session.Story `json:"story"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	entry := daemon.SessionEntry{Host: u.Hostname(), Port: port}

	st := &session.Story{Version: 7}
	if err := postStoryToDaemon(entry, st); err != nil {
		t.Fatalf("postStoryToDaemon: %v", err)
	}
	if gotPath != "/api/story" {
		t.Errorf("expected POST to /api/story, got %q", gotPath)
	}
	if gotBody.Story == nil || gotBody.Story.Version != 7 {
		t.Errorf("body must be {\"story\": {...}} with the story, got %+v", gotBody.Story)
	}
}

// TestPostStoryToDaemon_PollsReadinessThenPosts verifies the readiness poll:
// the daemon 503s on /api/session N times (session init not done), and
// postStoryToDaemon must NOT POST the story until /api/session stops 503ing.
func TestPostStoryToDaemon_PollsReadinessThenPosts(t *testing.T) {
	const notReadyTimes = 3
	var sessionHits int
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			sessionHits++
			if sessionHits <= notReadyTimes {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/api/story":
			// The story must only be posted AFTER readiness (session no longer 503).
			if sessionHits <= notReadyTimes {
				t.Errorf("posted story before daemon was ready (session hits=%d)", sessionHits)
			}
			posted = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	entry := daemon.SessionEntry{Host: u.Hostname(), Port: port}

	if err := postStoryToDaemon(entry, &session.Story{Version: 1}); err != nil {
		t.Fatalf("postStoryToDaemon should succeed after the daemon becomes ready: %v", err)
	}
	if sessionHits < notReadyTimes+1 {
		t.Errorf("expected the readiness poll to retry through %d 503s, got %d /api/session hits", notReadyTimes, sessionHits)
	}
	if !posted {
		t.Error("story was never POSTed after the daemon became ready")
	}
}

func TestPostStoryToDaemon_RejectsNonReadinessError(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			http.Error(w, "broken", http.StatusInternalServerError)
			return
		}
		posted = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	err := postStoryToDaemon(daemon.SessionEntry{Host: u.Hostname(), Port: port}, &session.Story{Version: 1})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected readiness error, got %v", err)
	}
	if posted {
		t.Fatal("story mutation ran before daemon was ready")
	}
}

func TestDeleteStoryFromDaemon(t *testing.T) {
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			w.WriteHeader(http.StatusOK)
		case "/api/story":
			if r.Method != http.MethodDelete {
				t.Errorf("method = %s, want DELETE", r.Method)
			}
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := deleteStoryFromDaemon(daemon.SessionEntry{Host: u.Hostname(), Port: port}); err != nil {
		t.Fatalf("deleteStoryFromDaemon: %v", err)
	}
	if !deleted {
		t.Fatal("DELETE /api/story was not called")
	}
}

func TestDeleteStoryFromDaemonRejectsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "could not clear", http.StatusConflict)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	err := deleteStoryFromDaemon(daemon.SessionEntry{Host: u.Hostname(), Port: port})
	if err == nil || !strings.Contains(err.Error(), "could not clear") {
		t.Fatalf("expected daemon error body, got %v", err)
	}
}

// TestPostStoryToDaemon_ErrorOnNon2xx verifies a non-2xx daemon response is a
// returned error (so postIngest logs it as a note rather than silently
// succeeding).
func TestPostStoryToDaemon_ErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "coverage rejected", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	entry := daemon.SessionEntry{Host: u.Hostname(), Port: port}

	if err := postStoryToDaemon(entry, &session.Story{Version: 1}); err == nil {
		t.Fatal("expected an error on a non-2xx daemon response")
	}
}

// TestPostIngest_NotifiesRunningDaemon verifies the decision logic: when a
// daemon is alive for the session key, postIngest POSTs the story to it and
// does NOT spawn a new daemon or open a browser.
func TestPostIngest_NotifiesRunningDaemon(t *testing.T) {
	setupStoryRepoLLM(t)

	origAlive := storyDaemonAlive
	origStart := storyStartDaemon
	origPost := storyPostStory
	origOpen := openBrowser
	t.Cleanup(func() {
		storyDaemonAlive = origAlive
		storyStartDaemon = origStart
		storyPostStory = origPost
		openBrowser = origOpen
	})

	var posted bool
	var spawned bool
	var opened bool
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		return daemon.SessionEntry{Port: 12345}, true
	}
	storyPostStory = func(daemon.SessionEntry, *session.Story) error { posted = true; return nil }
	storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) { spawned = true; return daemon.SessionEntry{}, nil }
	openBrowser = func(string, string) { opened = true }

	postIngest(storyFlags{}, sessionStoryStub())

	if !posted {
		t.Error("expected story to be POSTed to the running daemon")
	}
	if spawned {
		t.Error("must NOT spawn a daemon when one is already running")
	}
	if opened {
		t.Error("must NOT open the browser when a daemon is already running")
	}
}

// TestPostIngest_SpawnsWhenNoDaemon verifies the no-daemon branch spawns a
// daemon and opens the browser (respecting --no-open).
func TestPostIngest_SpawnsWhenNoDaemon(t *testing.T) {
	setupStoryRepoLLM(t)

	origAlive := storyDaemonAlive
	origStart := storyStartDaemon
	origPost := storyPostStory
	origOpen := openBrowser
	origHasBrowser := storyDaemonHasBrowser
	t.Cleanup(func() {
		storyDaemonAlive = origAlive
		storyStartDaemon = origStart
		storyPostStory = origPost
		openBrowser = origOpen
		storyDaemonHasBrowser = origHasBrowser
	})

	var spawned, opened bool
	var openedURL string
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
	storyStartDaemon = func(string, []string) (daemon.SessionEntry, error) {
		spawned = true
		return daemon.SessionEntry{Port: 6789}, nil
	}
	storyDaemonHasBrowser = func(daemon.SessionEntry) bool { return false }
	openBrowser = func(u, _ string) { opened = true; openedURL = u }

	// Default flags (no --no-open): browser opens at the story view.
	postIngest(storyFlags{}, sessionStoryStub())
	if !spawned {
		t.Error("expected a daemon to be spawned when none is running")
	}
	if !opened {
		t.Error("expected the browser to open on spawn")
	}
	if !strings.HasSuffix(openedURL, "#story") {
		t.Fatalf("fresh-ingest spawn must open the browser at #story, got %q", openedURL)
	}

	// --no-open: no browser.
	spawned, opened = false, false
	postIngest(storyFlags{noOpen: true}, sessionStoryStub())
	if !spawned {
		t.Error("expected a daemon spawn even with --no-open")
	}
	if opened {
		t.Error("--no-open must suppress the browser")
	}
}
