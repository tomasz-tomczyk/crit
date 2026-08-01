package comment

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
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
	if _, err := os.Stat(filepath.Join(configuredOutput, "reviews")); !os.IsNotExist(err) {
		// Only fail if a review.json was written under the configured root.
		cwd, err := daemon.ResolvedCWD()
		if err != nil {
			t.Fatal(err)
		}
		configuredIdentity := filepath.Join(configuredOutput, "reviews", daemon.SessionKey(cwd, "", nil))
		if _, err := os.Stat(filepath.Join(configuredIdentity, "review.json")); !os.IsNotExist(err) {
			t.Fatalf("configured review unexpectedly written while daemon active: %v", err)
		}
	}

	health.Close()
	daemon.RemoveSessionFile(sessionKey)
	if err := RunComment([]string{"--author", "bot", "configured fallback"}); err != nil {
		t.Fatal(err)
	}
	cwd, err = daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	configuredReview, err := review.LoadCritJSON(filepath.Join(configuredOutput, "reviews", daemon.SessionKey(cwd, "", nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(configuredReview.ReviewComments) != 1 || configuredReview.ReviewComments[0].Body != "configured fallback" {
		t.Fatalf("configured review comments = %+v, want fallback comment", configuredReview.ReviewComments)
	}
}

func TestRunCommentRequiresSessionWhenTwoReviewsMatch(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		if r.URL.Path == "/api/session" {
			fmt.Fprint(w, `{"focus":null}`)
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
	branch := vcs.CurrentBranch()
	const firstKey = "111111111111"
	const secondKey = "222222222222"
	firstPath := filepath.Join(t.TempDir(), firstKey)
	secondPath := filepath.Join(t.TempDir(), secondKey)
	for _, reviewPath := range []string{firstPath, secondPath} {
		if err := review.SaveCritJSON(reviewPath, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}}); err != nil {
			t.Fatal(err)
		}
	}
	for key, entry := range map[string]daemon.SessionEntry{
		firstKey:  {PID: os.Getpid(), Port: port, CWD: cwd, Branch: branch, Args: []string{"one.md"}, ReviewPath: firstPath},
		secondKey: {PID: os.Getpid(), Port: port, CWD: cwd, Branch: branch, Args: []string{"two.md"}, ReviewPath: secondPath},
	} {
		if err := daemon.WriteSessionFile(key, entry); err != nil {
			t.Fatal(err)
		}
	}

	err = RunComment([]string{"--author", "bot", "ambiguous"})
	if err == nil || !strings.Contains(err.Error(), "multiple active review sessions") {
		t.Fatalf("RunComment error = %v, want ambiguity", err)
	}

	if err := RunComment([]string{"--session", secondKey, "--author", "bot", "targeted"}); err != nil {
		t.Fatal(err)
	}
	bulkFile := filepath.Join(t.TempDir(), "comments.json")
	if err := os.WriteFile(bulkFile, []byte(`[{"body":"bulk targeted"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunComment([]string{"--session", secondKey, "--json", "--file", bulkFile, "--author", "bot"}); err != nil {
		t.Fatal(err)
	}
	first, err := review.LoadCritJSON(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := review.LoadCritJSON(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ReviewComments) != 0 {
		t.Fatalf("first review comments = %+v, want none", first.ReviewComments)
	}
	if len(second.ReviewComments) != 2 || second.ReviewComments[0].Body != "targeted" || second.ReviewComments[1].Body != "bulk targeted" {
		t.Fatalf("second review comments = %+v, want targeted single and bulk comments", second.ReviewComments)
	}
}
