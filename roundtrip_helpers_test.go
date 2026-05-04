//go:build e2e_github

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// roundtripEnv holds the per-scenario state.
type roundtripEnv struct {
	t          *testing.T
	repoSlug   string // e.g. "crit-md/crit-roundtrip-sandbox"
	branch     string // unique per scenario
	prNumber   int
	workDir    string // temp clone dir
	critBinary string
}

// newRoundtripEnv clones the sandbox into a temp dir, creates a unique
// branch with a tiny diff against main, opens a PR, and registers cleanup
// to close the PR + delete the branch. Skips when prerequisites are missing.
func newRoundtripEnv(t *testing.T) *roundtripEnv {
	t.Helper()
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not installed")
	}
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		t.Skip("gh not authenticated")
	}
	repo := os.Getenv("CRIT_ROUNDTRIP_REPO")
	if repo == "" {
		t.Skip("CRIT_ROUNDTRIP_REPO not set")
	}
	bin := os.Getenv("CRIT_BINARY")
	if bin == "" {
		t.Skip("CRIT_BINARY not set")
	}

	dir := t.TempDir()
	branch := fmt.Sprintf("rt-%s-%d", sanitizeName(t.Name()), time.Now().UnixNano())

	cloneURL := fmt.Sprintf("git@github.com:%s.git", repo)
	if u := os.Getenv("CRIT_ROUNDTRIP_CLONE_URL"); u != "" {
		cloneURL = u
	}
	mustRun(t, dir, "git", "clone", "--depth", "1", cloneURL, ".")
	mustRun(t, dir, "git", "config", "user.email", "me@tomasztomczyk.com")
	mustRun(t, dir, "git", "config", "user.name", "crit-roundtrip-bot")
	mustRun(t, dir, "git", "checkout", "-b", branch)

	if err := appendLine(filepath.Join(dir, "sample.go"), "\nfunc Mod(a, b int) int { return a % b }\n"); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(filepath.Join(dir, "sample.md"), "\n## Section D\nFirst paragraph in section D.\n"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "commit", "-am", "add Mod and section D")
	mustRun(t, dir, "git", "push", "-u", "origin", branch)

	prURL := mustOutput(t, dir, "gh", "pr", "create",
		"--repo", repo,
		"--title", "roundtrip: "+t.Name(),
		"--body", "automated test PR",
		"--head", branch,
		"--base", "main")
	prNum := mustParsePRNumber(t, prURL)

	env := &roundtripEnv{
		t: t, repoSlug: repo, branch: branch, prNumber: prNum,
		workDir: dir, critBinary: bin,
	}

	t.Cleanup(func() {
		_ = exec.Command("gh", "pr", "close",
			"--repo", repo, fmt.Sprint(prNum), "--delete-branch").Run()
	})
	return env
}

// runCrit runs the crit binary inside the env workdir with the given args
// and returns combined stdout+stderr. Fails the test on non-zero exit.
func (e *roundtripEnv) runCrit(args ...string) string {
	e.t.Helper()
	cmd := exec.Command(e.critBinary, args...)
	cmd.Dir = e.workDir
	cmd.Env = append(os.Environ(), "GH_NO_UPDATE_NOTIFIER=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		e.t.Fatalf("crit %v failed: %v\noutput:\n%s",
			args, err, out.String())
	}
	return out.String()
}

func (e *roundtripEnv) runCritExpectExit(args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(e.critBinary, args...)
	cmd.Dir = e.workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		e.t.Fatalf("crit %v: %v", args, err)
	}
	return out.String(), code
}

// reviewFile reads and unmarshals the review file crit wrote for this branch
// via `crit status --json`.
func (e *roundtripEnv) reviewFile() CritJSON {
	e.t.Helper()
	out := e.runCrit("status", "--json")
	var status struct {
		ReviewPath string `json:"review_path"`
		ReviewFile string `json:"review_file"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		e.t.Fatalf("parse status JSON: %v\noutput:\n%s", err, out)
	}
	path := status.ReviewPath
	if path == "" {
		path = status.ReviewFile
	}
	if path == "" {
		e.t.Fatalf("status JSON had no review_path/review_file:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("read review file %s: %v", path, err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		e.t.Fatalf("parse review file: %v", err)
	}
	return cj
}

// editReviewFile loads the review file, applies mutate, and saves it.
// Use to simulate what the daemon API would do for body edits / resolved flips.
func (e *roundtripEnv) editReviewFile(mutate func(cj *CritJSON)) {
	e.t.Helper()
	out := e.runCrit("status", "--json")
	var status struct {
		ReviewPath string `json:"review_path"`
		ReviewFile string `json:"review_file"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		e.t.Fatalf("parse status JSON: %v\noutput:\n%s", err, out)
	}
	path := status.ReviewPath
	if path == "" {
		path = status.ReviewFile
	}
	if path == "" {
		e.t.Fatalf("status JSON had neither review_path nor review_file:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("read review file: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		e.t.Fatalf("parse review file: %v", err)
	}
	mutate(&cj)
	out2, err := json.MarshalIndent(&cj, "", "  ")
	if err != nil {
		e.t.Fatalf("marshal review file: %v", err)
	}
	if err := os.WriteFile(path, out2, 0644); err != nil {
		e.t.Fatalf("write review file: %v", err)
	}
}

// localComment is a flat (file, comment) view of the review file.
type localComment struct {
	Path    string
	Comment Comment
}

func (e *roundtripEnv) allLocalComments() []localComment {
	cj := e.reviewFile()
	var out []localComment
	for path, f := range cj.Files {
		for _, c := range f.Comments {
			out = append(out, localComment{Path: path, Comment: c})
		}
	}
	return out
}

// remoteComment is a slim view of the GitHub review-comment API payload.
type remoteComment struct {
	ID        int64  `json:"id"`
	InReplyTo int64  `json:"in_reply_to_id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line"`
	Body      string `json:"body"`
	UserLogin string `json:"-"`
	RawUser   struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (e *roundtripEnv) listRemoteComments() []remoteComment {
	e.t.Helper()
	out := mustOutput(e.t, e.workDir, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", e.repoSlug, e.prNumber))
	var raw []remoteComment
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		e.t.Fatalf("parse remote comments: %v\n%s", err, out)
	}
	for i := range raw {
		raw[i].UserLogin = raw[i].RawUser.Login
	}
	return raw
}

func (e *roundtripEnv) postRemoteComment(path string, line int, body string) int64 {
	e.t.Helper()
	headSHA := strings.TrimSpace(mustOutput(e.t, e.workDir,
		"git", "rev-parse", "HEAD"))
	payload := map[string]any{
		"body":      body,
		"commit_id": headSHA,
		"path":      path,
		"line":      line,
		"side":      "RIGHT",
	}
	buf, _ := json.Marshal(payload)
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d/comments", e.repoSlug, e.prNumber),
		"--method", "POST", "--input", "-")
	cmd.Stdin = bytes.NewReader(buf)
	cmd.Dir = e.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("post remote comment: %v\n%s", err, out)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		e.t.Fatalf("parse remote-comment response: %v\n%s", err, out)
	}
	return resp.ID
}

func (e *roundtripEnv) postRemoteReply(parentID int64, body string) int64 {
	e.t.Helper()
	payload := map[string]any{"body": body, "in_reply_to": parentID}
	buf, _ := json.Marshal(payload)
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d/comments", e.repoSlug, e.prNumber),
		"--method", "POST", "--input", "-")
	cmd.Stdin = bytes.NewReader(buf)
	cmd.Dir = e.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("post remote reply: %v\n%s", err, out)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		e.t.Fatalf("parse remote-reply response: %v\n%s", err, out)
	}
	return resp.ID
}

// --- low-level helpers ---

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed in %s: %v\n%s", name, args, dir, err, out)
	}
}

func mustOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}

func appendLine(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

func mustParsePRNumber(t *testing.T, prURL string) int {
	t.Helper()
	prURL = strings.TrimSpace(prURL)
	idx := strings.LastIndex(prURL, "/")
	if idx < 0 {
		t.Fatalf("cannot parse PR number from %q", prURL)
	}
	n, err := strconv.Atoi(prURL[idx+1:])
	if err != nil {
		t.Fatalf("cannot parse PR number from %q: %v", prURL, err)
	}
	return n
}

// freshClone makes another clone of the same PR's branch in a new tempdir,
// independent of the original env's workdir/review-file state. Use to
// simulate "user B" picking up a PR.
func (e *roundtripEnv) freshClone() *roundtripEnv {
	e.t.Helper()
	dir := e.t.TempDir()
	cloneURL := fmt.Sprintf("git@github.com:%s.git", e.repoSlug)
	if v := os.Getenv("CRIT_ROUNDTRIP_CLONE_URL"); v != "" {
		cloneURL = v
	}
	mustRun(e.t, dir, "git", "clone", "--depth", "1", "--branch", e.branch,
		cloneURL, ".")
	mustRun(e.t, dir, "git", "config", "user.email", "userb@example.com")
	mustRun(e.t, dir, "git", "config", "user.name", "user-b")
	return &roundtripEnv{
		t: e.t, repoSlug: e.repoSlug, branch: e.branch, prNumber: e.prNumber,
		workDir: dir, critBinary: e.critBinary,
	}
}

// waitForPRHeadSHA polls the PR's head.sha until it matches expected, or
// fails the test after a timeout. Use after a force-push to give GitHub
// time to recompute the PR diff before the next push fires; otherwise the
// next POST /pulls/{N}/comments may be rejected with HTTP 422 because the
// commit_id no longer matches the PR's recomputed diff.
func (e *roundtripEnv) waitForPRHeadSHA(expected string) {
	e.t.Helper()
	expected = strings.TrimSpace(expected)
	deadline := time.Now().Add(15 * time.Second)
	var lastSHA string
	for time.Now().Before(deadline) {
		out := mustOutput(e.t, e.workDir, "gh", "api",
			fmt.Sprintf("repos/%s/pulls/%d", e.repoSlug, e.prNumber))
		var resp struct {
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		}
		if err := json.Unmarshal([]byte(out), &resp); err == nil {
			lastSHA = resp.Head.SHA
			if lastSHA == expected {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.t.Fatalf("PR head sha did not converge to %s within timeout (last seen: %s)",
		expected, lastSHA)
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "-")
	if len(s) > 30 {
		s = s[:30]
	}
	return s
}
