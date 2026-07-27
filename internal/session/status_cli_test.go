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
	t.Setenv("HOME", t.TempDir())
	t.Chdir(projectDir)
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, outputDir)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeStatusReview(t, filepath.Join(outputDir, ".crit"))

	result := captureStatusJSON(t)
	want := filepath.Join(outputDir, ".crit", "review.json")
	if result["review_file"] != want {
		t.Fatalf("review_file = %q, want %q", result["review_file"], want)
	}
	if result["review_file_exists"] != true {
		t.Fatalf("review_file_exists = %v, want true", result["review_file_exists"])
	}
}

func TestResolveStatusReviewPathCentralizedFallback(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("HOME", t.TempDir())
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
	t.Setenv("HOME", homeDir)
	t.Chdir(projectDir)
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, configuredOutput)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeStatusReview(t, filepath.Join(configuredOutput, ".crit"))

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

func TestRunStatusFindsRepoRootSessionFromNestedDirectory(t *testing.T) {
	repoDir := testutil.InitTestRepo(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configuredOutput := filepath.Join(repoDir, "configured-output")
	if err := os.WriteFile(
		filepath.Join(repoDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, configuredOutput)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeStatusReview(t, filepath.Join(configuredOutput, ".crit"))

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
