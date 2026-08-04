package session

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func captureStatusJSON(t *testing.T) map[string]interface{} {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	if err := RunStatus([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decoding status JSON %q: %v", data, err)
	}
	return result
}

func captureStatusHuman(t *testing.T) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })
	if err := RunStatus(nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeStatusReview(t *testing.T, reviewPath string) {
	t.Helper()
	if err := os.MkdirAll(reviewPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		ReviewPathsFor(reviewPath).Review,
		[]byte(`{"version":4,"review_round":1,"files":{}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestRunStatusUsesConfiguredOutputWithoutDaemon(t *testing.T) {
	projectDir := t.TempDir()
	outputDir := filepath.Join(projectDir, "configured-output")
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, outputDir)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	key := daemon.SessionKey(cwd, "", nil)
	identity := filepath.Join(outputDir, "reviews", key)
	writeStatusReview(t, identity)

	result := captureStatusJSON(t)
	want := filepath.Join(identity, "review.json")
	if result["review_file"] != want {
		t.Fatalf("review_file = %q, want %q", result["review_file"], want)
	}
	if result["review_file_exists"] != true {
		t.Fatalf("review_file_exists = %v, want true", result["review_file_exists"])
	}
}

func TestResolveStatusReviewPathCentralizedFallback(t *testing.T) {
	cwd := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(cwd)
	resolvedCWD, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveStatusReviewPath(resolvedCWD, "feature", nil)
	if err != nil {
		t.Fatal(err)
	}
	key := daemon.SessionKey(resolvedCWD, "feature", nil)
	want, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("review path = %q, want centralized path %q", got, want)
	}
}

func TestRunStatusLiveSessionWinsOverConfiguredOutput(t *testing.T) {
	projectDir := t.TempDir()
	configuredOutput := filepath.Join(projectDir, "configured-output")
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	t.Chdir(projectDir)
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, configuredOutput)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeStatusReview(t, filepath.Join(configuredOutput, "reviews", "unused"))

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	t.Cleanup(health.Close)
	parsed, err := url.Parse(health.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	liveReviewPath := filepath.Join(t.TempDir(), ".crit")
	writeStatusReview(t, liveReviewPath)
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.WriteSessionFile("status-live", daemon.SessionEntry{
		PID:        os.Getpid(),
		Port:       port,
		CWD:        cwd,
		ReviewPath: liveReviewPath,
	}); err != nil {
		t.Fatal(err)
	}

	result := captureStatusJSON(t)
	want := filepath.Join(liveReviewPath, "review.json")
	if result["review_file"] != want {
		t.Fatalf("review_file = %q, want live session path %q", result["review_file"], want)
	}
}

func TestRunStatusListsAllMatchingSessions(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	t.Cleanup(health.Close)
	parsed, err := url.Parse(health.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	backend := vcs.DetectVCS("")
	if backend == nil {
		t.Fatal("expected git repository")
	}

	const firstKey = "111111111111"
	const secondKey = "222222222222"
	const otherBranchKey = "333333333333"
	for key, testSession := range map[string]struct {
		args   []string
		branch string
	}{
		firstKey:       {args: []string{"one.md"}, branch: backend.CurrentBranch()},
		secondKey:      {args: []string{"two.md"}, branch: backend.CurrentBranch()},
		otherBranchKey: {args: []string{"other.md"}, branch: "other-branch"},
	} {
		reviewPath := filepath.Join(t.TempDir(), key)
		writeStatusReview(t, reviewPath)
		if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
			PID: os.Getpid(), Port: port, CWD: cwd, Branch: testSession.branch, Args: testSession.args, ReviewPath: reviewPath,
		}); err != nil {
			t.Fatal(err)
		}
	}

	result := captureStatusJSON(t)
	sessions, ok := result["sessions"].([]interface{})
	if !ok || len(sessions) != 2 {
		t.Fatalf("sessions = %#v, want two entries", result["sessions"])
	}
	got := map[string]bool{}
	for _, raw := range sessions {
		entry := raw.(map[string]interface{})
		got[entry["id"].(string)] = true
	}
	if !got[firstKey] || !got[secondKey] {
		t.Fatalf("session IDs = %v, want %s and %s", got, firstKey, secondKey)
	}
	if got[otherBranchKey] {
		t.Fatalf("session IDs = %v, should not include other branch %s", got, otherBranchKey)
	}
	if result["review_file"] != nil {
		t.Fatalf("review_file = %#v, want nil when multiple sessions match", result["review_file"])
	}
	if note, _ := result["note"].(string); !strings.Contains(note, "multiple active review sessions") {
		t.Fatalf("note = %#v, want ambiguity note", result["note"])
	}
	daemonStatus, ok := result["daemon"].(map[string]interface{})
	if !ok {
		t.Fatalf("daemon = %#v, want object", result["daemon"])
	}
	if daemonStatus["running"] != false {
		t.Fatalf("daemon.running = %v, want false when ambiguous", daemonStatus["running"])
	}
	human := captureStatusHuman(t)
	for _, want := range []string{"Active reviews: 2", firstKey, "one.md", secondKey, "two.md", "ambiguous"} {
		if !strings.Contains(human, want) {
			t.Fatalf("status output %q does not contain %q", human, want)
		}
	}
}

func TestRunStatusFindsRepoRootSessionFromNestedDirectory(t *testing.T) {
	repoDir := testutil.InitTestRepo(t)
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	configuredOutput := filepath.Join(repoDir, "configured-output")
	if err := os.WriteFile(
		filepath.Join(repoDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, configuredOutput)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeStatusReview(t, filepath.Join(configuredOutput, "reviews", "unused"))

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	t.Cleanup(health.Close)
	parsed, err := url.Parse(health.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(repoDir)
	backend := vcs.DetectVCS("")
	if backend == nil {
		t.Fatal("expected git repository")
	}
	repoRoot, err := backend.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rootReviewPath := filepath.Join(t.TempDir(), ".crit")
	writeStatusReview(t, rootReviewPath)
	const sessionKey = "status-repo-root"
	if err := daemon.WriteSessionFile(sessionKey, daemon.SessionEntry{
		PID:        os.Getpid(),
		Port:       port,
		CWD:        repoRoot,
		Branch:     backend.CurrentBranch(),
		ReviewPath: rootReviewPath,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.RemoveSessionFile(sessionKey) })

	nestedDir := filepath.Join(repoDir, "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nestedDir)
	nestedCWD, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	const wrongBranchSessionKey = "status-nested-wrong-branch"
	if err := daemon.WriteSessionFile(wrongBranchSessionKey, daemon.SessionEntry{
		PID:        os.Getpid(),
		Port:       port,
		CWD:        nestedCWD,
		Branch:     "different-branch",
		ReviewPath: filepath.Join(t.TempDir(), ".crit"),
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.RemoveSessionFile(wrongBranchSessionKey) })

	result := captureStatusJSON(t)
	want := filepath.Join(rootReviewPath, "review.json")
	if result["review_file"] != want {
		t.Fatalf("review_file = %q, want root daemon path %q", result["review_file"], want)
	}
	daemonStatus, ok := result["daemon"].(map[string]interface{})
	if !ok {
		t.Fatalf("daemon status = %#v, want object", result["daemon"])
	}
	if daemonStatus["running"] != true {
		t.Fatalf("daemon.running = %v, want true", daemonStatus["running"])
	}
}

func TestSelectStatusSession(t *testing.T) {
	if got := selectStatusSession(nil); got != nil {
		t.Fatalf("empty = %#v, want nil", got)
	}
	if got := selectStatusSession([]daemon.SessionEntry{{}, {}}); got != nil {
		t.Fatalf("ambiguous = %#v, want nil", got)
	}
	sole := []daemon.SessionEntry{{PID: 42, Port: 3000, ReviewPath: "/tmp/r"}}
	got := selectStatusSession(sole)
	if got == nil || got.PID != 42 || got.Port != 3000 {
		t.Fatalf("sole = %#v, want first entry", got)
	}
}

func TestLoadStatusSessionsAmbiguous(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	t.Cleanup(health.Close)
	parsed, err := url.Parse(health.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	backend := vcs.DetectVCS("")
	if backend == nil {
		t.Fatal("expected git repository")
	}
	branch := backend.CurrentBranch()

	const firstKey = "aaaaaaaaaaaa"
	const secondKey = "bbbbbbbbbbbb"
	for _, key := range []string{firstKey, secondKey} {
		if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
			PID: os.Getpid(), Port: port, CWD: cwd, Branch: branch,
			Args: []string{key + ".md"}, ReviewPath: filepath.Join(t.TempDir(), key),
		}); err != nil {
			t.Fatal(err)
		}
		k := key
		t.Cleanup(func() { daemon.RemoveSessionFile(k) })
	}

	sessions, keys, matched, err := loadStatusSessions(cwd, branch, backend)
	if err != nil {
		t.Fatal(err)
	}
	if matched != nil {
		t.Fatalf("matched = %#v, want nil when ambiguous", matched)
	}
	if len(sessions) != 2 || len(keys) != 2 {
		t.Fatalf("sessions=%v keys=%v, want two matches", sessions, keys)
	}
}
