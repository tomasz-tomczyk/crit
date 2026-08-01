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
	"sync"
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
	err = RunComment([]string{"--output", t.TempDir(), "--author", "bot", "still ambiguous"})
	if err == nil || !strings.Contains(err.Error(), "multiple active review sessions") {
		t.Fatalf("RunComment --output error = %v, want ambiguity because output selects storage, not a session", err)
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

func TestRunCommentIgnoresSessionsFromOtherBranches(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	for key, branch := range map[string]string{
		"111111111111": "feature-a",
		"222222222222": "feature-b",
	} {
		if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
			PID: os.Getpid(), Port: port, CWD: cwd, Branch: branch, ReviewPath: filepath.Join(t.TempDir(), key),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunComment([]string{"--author", "bot", "current branch"}); err != nil {
		t.Fatal(err)
	}
	key := daemon.SessionKey(cwd, vcs.CurrentBranch(), nil)
	reviewPath, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	cj, err := review.LoadCritJSON(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 || cj.ReviewComments[0].Body != "current branch" {
		t.Fatalf("centralized review comments = %+v, want current-branch comment", cj.ReviewComments)
	}
}

func TestRunCommentSessionErrors(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	t.Chdir(t.TempDir())
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing value", args: []string{"--session"}, wantErr: "requires a value"},
		{name: "invalid ID", args: []string{"--session", "invalid", "body"}, wantErr: "invalid session ID"},
		{name: "inactive ID", args: []string{"--session", "aaaaaaaaaaaa", "body"}, wantErr: "no active review session"},
		{name: "output conflict", args: []string{"--session", "aaaaaaaaaaaa", "--output", t.TempDir(), "body"}, wantErr: "cannot be used with"},
		{name: "plan conflict", args: []string{"--session", "aaaaaaaaaaaa", "--plan", "plan", "body"}, wantErr: "cannot be used with"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunComment(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunComment error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunCommentSessionRegistrySwapKeepsPathAndFocusCoherent(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	if err := os.WriteFile(filepath.Join(projectDir, "file.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const key = "aaaaaaaaaaaa"
	firstPath := filepath.Join(t.TempDir(), "first")
	secondPath := filepath.Join(t.TempDir(), "second")
	for _, reviewPath := range []string{firstPath, secondPath} {
		if err := review.SaveCritJSON(reviewPath, session.CritJSON{
			ReviewRound: 1,
			Files:       map[string]session.CritJSONFile{"file.go": {}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/session":
			fmt.Fprint(w, `{"focus":{"kind":"range","head_sha":"head-b","diff_scope":"layer"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(secondServer.Close)
	secondURL, err := url.Parse(secondServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	secondPort, err := strconv.Atoi(secondURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	secondEntry := daemon.SessionEntry{PID: os.Getpid(), Port: secondPort, CWD: cwd, Branch: vcs.CurrentBranch(), ReviewPath: secondPath}

	var swapOnce sync.Once
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			swapOnce.Do(func() { _ = daemon.WriteSessionFile(key, secondEntry) })
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/session":
			fmt.Fprint(w, `{"focus":{"kind":"range","head_sha":"head-a","diff_scope":"layer"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(firstServer.Close)
	firstURL, err := url.Parse(firstServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	firstPort, err := strconv.Atoi(firstURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
		PID: os.Getpid(), Port: firstPort, CWD: cwd, Branch: vcs.CurrentBranch(), ReviewPath: firstPath,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunComment([]string{"--session", key, "--scope", "layer", "--author", "bot", "file.go:1", "coherent"}); err != nil {
		t.Fatal(err)
	}
	first, err := review.LoadCritJSON(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	comments := first.Files["file.go"].Comments
	if len(comments) != 1 || comments[0].HeadSHA != "head-a" {
		t.Fatalf("first review comments = %+v, want one comment with head-a", comments)
	}
	second, err := review.LoadCritJSON(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Files["file.go"].Comments) != 0 {
		t.Fatalf("second review comments = %+v, want none", second.Files["file.go"].Comments)
	}
}

func TestRunCommentUnqualifiedSessionKeepsPathAndFocusCoherent(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)
	if err := os.WriteFile(filepath.Join(projectDir, "file.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	startFocusServer := func(head string) (*httptest.Server, int) {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/health":
				fmt.Fprint(w, `{"status":"ok"}`)
			case "/api/session":
				fmt.Fprintf(w, `{"focus":{"kind":"range","head_sha":%q,"diff_scope":"layer"}}`, head)
			default:
				http.NotFound(w, r)
			}
		}))
		parsed, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal(err)
		}
		return server, port
	}

	firstServer, firstPort := startFocusServer("head-current")
	t.Cleanup(firstServer.Close)
	secondServer, secondPort := startFocusServer("head-other")
	t.Cleanup(secondServer.Close)
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(t.TempDir(), "current")
	otherPath := filepath.Join(t.TempDir(), "other")
	for _, reviewPath := range []string{currentPath, otherPath} {
		if err := review.SaveCritJSON(reviewPath, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{"file.go": {}}}); err != nil {
			t.Fatal(err)
		}
	}
	entries := map[string]daemon.SessionEntry{
		"111111111111": {PID: os.Getpid(), Port: firstPort, CWD: cwd, Branch: vcs.CurrentBranch(), ReviewPath: currentPath},
		"222222222222": {PID: os.Getpid(), Port: secondPort, CWD: cwd, Branch: "other-branch", ReviewPath: otherPath},
	}
	for key, entry := range entries {
		if err := daemon.WriteSessionFile(key, entry); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunComment([]string{"--scope", "layer", "--author", "bot", "file.go:1", "current focus"}); err != nil {
		t.Fatal(err)
	}
	cj, err := review.LoadCritJSON(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	comments := cj.Files["file.go"].Comments
	if len(comments) != 1 || comments[0].HeadSHA != "head-current" {
		t.Fatalf("current review comments = %+v, want head-current", comments)
	}
}

func TestRunCommentExplicitSessionDoesNotRedirectReplies(t *testing.T) {
	projectDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	t.Chdir(projectDir)

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/session":
			fmt.Fprint(w, `{"focus":null}`)
		default:
			http.NotFound(w, r)
		}
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
	const selectedKey = "aaaaaaaaaaaa"
	const siblingKey = "bbbbbbbbbbbb"
	selectedPath, err := daemon.ReviewFilePath(selectedKey)
	if err != nil {
		t.Fatal(err)
	}
	siblingPath, err := daemon.ReviewFilePath(siblingKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := review.SaveCritJSON(selectedPath, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}
	if err := review.SaveCritJSON(siblingPath, session.CritJSON{
		ReviewRound:    1,
		Files:          map[string]session.CritJSONFile{},
		ReviewComments: []session.Comment{{ID: "r_sibling", Body: "sibling", Scope: "review"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.WriteSessionFile(selectedKey, daemon.SessionEntry{
		PID: os.Getpid(), Port: port, CWD: cwd, Branch: vcs.CurrentBranch(), ReviewPath: selectedPath,
	}); err != nil {
		t.Fatal(err)
	}

	err = RunComment([]string{"--session", selectedKey, "--reply-to", "r_sibling", "--author", "bot", "single"})
	if err == nil || !strings.Contains(err.Error(), "not found in review file") {
		t.Fatalf("single reply error = %v, want selected-review not-found error", err)
	}
	bulkFile := filepath.Join(t.TempDir(), "replies.json")
	if err := os.WriteFile(bulkFile, []byte(`[{"reply_to":"r_sibling","body":"bulk"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	err = RunComment([]string{"--session", selectedKey, "--json", "--file", bulkFile, "--author", "bot"})
	if err == nil || !strings.Contains(err.Error(), "not found in selected review file") {
		t.Fatalf("bulk reply error = %v, want selected-review not-found error", err)
	}
	sibling, err := review.LoadCritJSON(siblingPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sibling.ReviewComments[0].Replies) != 0 {
		t.Fatalf("sibling replies = %+v, want none", sibling.ReviewComments[0].Replies)
	}
}
