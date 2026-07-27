package comment

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestRunCommentDaemonThenConfiguredOutputPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	daemonOutput := filepath.Join(projectDir, "daemon-output")
	configuredOutput := filepath.Join(projectDir, "configured-output")
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	if err := os.WriteFile(
		filepath.Join(projectDir, ".crit.config.json"),
		[]byte(fmt.Sprintf(`{"output":%q}`, configuredOutput)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	daemonReviewPath := filepath.Join(daemonOutput, ".crit")
	if err := review.SaveCritJSON(daemonReviewPath, session.CritJSON{
		ReviewRound: 1,
		Files:       map[string]session.CritJSONFile{},
	}); err != nil {
		t.Fatal(err)
	}

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
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
	const sessionKey = "comment-precedence"
	if err := daemon.WriteSessionFile(sessionKey, daemon.SessionEntry{
		PID:        os.Getpid(),
		Port:       port,
		CWD:        cwd,
		ReviewPath: daemonReviewPath,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.RemoveSessionFile(sessionKey) })

	if err := RunComment([]string{"--author", "bot", "active daemon"}); err != nil {
		t.Fatal(err)
	}
	daemonReview, err := review.LoadCritJSON(daemonReviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(daemonReview.ReviewComments) != 1 || daemonReview.ReviewComments[0].Body != "active daemon" {
		t.Fatalf("daemon review comments = %+v, want active daemon comment", daemonReview.ReviewComments)
	}
	if _, err := os.Stat(filepath.Join(configuredOutput, ".crit", "review.json")); !os.IsNotExist(err) {
		t.Fatalf("configured review unexpectedly written while daemon active: %v", err)
	}

	health.Close()
	daemon.RemoveSessionFile(sessionKey)
	if err := RunComment([]string{"--author", "bot", "configured fallback"}); err != nil {
		t.Fatal(err)
	}
	configuredReview, err := review.LoadCritJSON(filepath.Join(configuredOutput, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(configuredReview.ReviewComments) != 1 || configuredReview.ReviewComments[0].Body != "configured fallback" {
		t.Fatalf("configured review comments = %+v, want fallback comment", configuredReview.ReviewComments)
	}
}
