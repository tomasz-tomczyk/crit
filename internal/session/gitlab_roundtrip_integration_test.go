//go:build e2e_gitlab

package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type gitLabRoundtripEnv struct {
	t          *testing.T
	project    string
	host       string
	branch     string
	mrIID      int
	workDir    string
	outputDir  string
	critBinary string
}

type gitLabRoundtripMR struct {
	IID      int    `json:"iid"`
	WebURL   string `json:"web_url"`
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		StartSHA string `json:"start_sha"`
		HeadSHA  string `json:"head_sha"`
	} `json:"diff_refs"`
}

type gitLabRoundtripDiscussion struct {
	ID    string `json:"id"`
	Notes []struct {
		ID       int64           `json:"id"`
		Body     string          `json:"body"`
		Resolved bool            `json:"resolved"`
		Position json.RawMessage `json:"position"`
	} `json:"notes"`
}

func newGitLabRoundtripEnv(t *testing.T) *gitLabRoundtripEnv {
	t.Helper()
	project := os.Getenv("CRIT_GITLAB_ROUNDTRIP_PROJECT")
	if project == "" {
		t.Skip("CRIT_GITLAB_ROUNDTRIP_PROJECT not set")
	}
	critBinary := os.Getenv("CRIT_BINARY")
	if critBinary == "" {
		t.Skip("CRIT_BINARY not set")
	}
	if _, err := exec.LookPath("glab"); err != nil {
		t.Skip("glab not installed")
	}
	host := os.Getenv("CRIT_GITLAB_ROUNDTRIP_HOST")
	authArgs := []string{"auth", "status"}
	if host != "" {
		authArgs = append(authArgs, "--hostname", host)
	}
	if err := exec.Command("glab", authArgs...).Run(); err != nil {
		t.Skip("glab not authenticated")
	}

	projectPath := "projects/" + url.PathEscape(project)
	projectOut := mustGitLabAPI(t, host, projectPath, "GET", nil)
	var projectInfo struct {
		DefaultBranch string `json:"default_branch"`
		WebURL        string `json:"web_url"`
	}
	if err := json.Unmarshal(projectOut, &projectInfo); err != nil || projectInfo.DefaultBranch == "" || projectInfo.WebURL == "" {
		t.Fatalf("reading GitLab sandbox project: %v", err)
	}

	branch := fmt.Sprintf("crit-gitlab-e2e-%d", time.Now().UnixNano())
	fileBody := "package roundtrip\n\nfunc GitLabRoundtrip() {}\n"
	mustGitLabAPI(t, host, projectPath+"/repository/commits", "POST", map[string]any{
		"branch":         branch,
		"start_branch":   projectInfo.DefaultBranch,
		"commit_message": "crit GitLab roundtrip fixture",
		"actions": []map[string]any{{
			"action": "create", "file_path": "crit_gitlab_roundtrip.go", "content": fileBody,
		}},
	})
	mrOut := mustGitLabAPI(t, host, projectPath+"/merge_requests", "POST", map[string]any{
		"source_branch": branch, "target_branch": projectInfo.DefaultBranch,
		"title": "Crit GitLab roundtrip " + branch, "remove_source_branch": true,
	})
	var mr gitLabRoundtripMR
	if err := json.Unmarshal(mrOut, &mr); err != nil || mr.IID <= 0 {
		t.Fatalf("reading created GitLab MR: %v", err)
	}

	t.Cleanup(func() {
		tryGitLabAPI(host, fmt.Sprintf("%s/merge_requests/%d", projectPath, mr.IID), "PUT", map[string]any{"state_event": "close"})
		tryGitLabAPI(host, projectPath+"/repository/branches/"+url.PathEscape(branch), "DELETE", nil)
	})

	for attempt := 0; attempt < 20; attempt++ {
		mrOut = mustGitLabAPI(t, host, fmt.Sprintf("%s/merge_requests/%d", projectPath, mr.IID), "GET", nil)
		if err := json.Unmarshal(mrOut, &mr); err == nil && mr.DiffRefs.BaseSHA != "" && mr.DiffRefs.StartSHA != "" && mr.DiffRefs.HeadSHA != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if mr.DiffRefs.BaseSHA == "" || mr.DiffRefs.StartSHA == "" || mr.DiffRefs.HeadSHA == "" {
		t.Fatal("GitLab MR diff refs did not become ready")
	}

	workDir := t.TempDir()
	mustRunGitLabRoundtrip(t, workDir, "git", "init")
	mustRunGitLabRoundtrip(t, workDir, "git", "remote", "add", "origin", strings.TrimSuffix(projectInfo.WebURL, "/")+".git")
	mustRunGitLabRoundtrip(t, workDir, "git", "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(workDir, "crit_gitlab_roundtrip.go"), []byte(fileBody), 0o644); err != nil {
		t.Fatal(err)
	}

	return &gitLabRoundtripEnv{
		t: t, project: project, host: host, branch: branch, mrIID: mr.IID,
		workDir: workDir, outputDir: t.TempDir(), critBinary: critBinary,
	}
}

func (e *gitLabRoundtripEnv) runCrit(args ...string) string {
	e.t.Helper()
	cmd := exec.Command(e.critBinary, args...)
	cmd.Dir = e.workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		e.t.Fatalf("crit %v failed: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

func (e *gitLabRoundtripEnv) reviewFile() CritJSON {
	e.t.Helper()
	path := ReviewPathsFor(filepath.Join(e.outputDir, ".crit")).Review
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

func (e *gitLabRoundtripEnv) editReviewFile(mutate func(*CritJSON)) {
	e.t.Helper()
	path := ReviewPathsFor(filepath.Join(e.outputDir, ".crit")).Review
	cj := e.reviewFile()
	mutate(&cj)
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *gitLabRoundtripEnv) discussions() []gitLabRoundtripDiscussion {
	e.t.Helper()
	endpoint := fmt.Sprintf("projects/%s/merge_requests/%d/discussions?per_page=100", url.PathEscape(e.project), e.mrIID)
	out := mustGitLabAPI(e.t, e.host, endpoint, "GET", nil)
	var discussions []gitLabRoundtripDiscussion
	if err := json.Unmarshal(out, &discussions); err != nil {
		e.t.Fatalf("parse GitLab discussions: %v", err)
	}
	diffDiscussions := discussions[:0]
	for _, discussion := range discussions {
		if len(discussion.Notes) > 0 && len(discussion.Notes[0].Position) > 0 && string(discussion.Notes[0].Position) != "null" {
			diffDiscussions = append(diffDiscussions, discussion)
		}
	}
	return diffDiscussions
}

func TestGitLabRoundtrip_FullCommentLifecycle(t *testing.T) {
	e := newGitLabRoundtripEnv(t)
	e.runCrit("comment", "--output", e.outputDir, "--author", "Crit E2E", "crit_gitlab_roundtrip.go:3", "initial GitLab comment")

	firstOut := e.runCrit("push", "--forge", "gitlab", "--output", e.outputDir, fmt.Sprint(e.mrIID))
	if !strings.Contains(firstOut, "Posted 1 comments") {
		t.Fatalf("first push did not create one comment:\n%s", firstOut)
	}
	local := e.reviewFile()
	comment := local.Files["crit_gitlab_roundtrip.go"].Comments[0]
	if comment.GitLabNoteID == 0 || comment.GitLabDiscussionID == "" {
		t.Fatalf("remote IDs not persisted: %+v", comment)
	}

	e.editReviewFile(func(cj *CritJSON) {
		file := cj.Files["crit_gitlab_roundtrip.go"]
		file.Comments[0].Body = "edited GitLab comment"
		cj.Files["crit_gitlab_roundtrip.go"] = file
	})
	e.runCrit("comment", "--output", e.outputDir, "--reply-to", comment.ID, "--resolve", "--author", "Crit E2E", "GitLab reply")
	secondOut := e.runCrit("push", "--forge", "gitlab", "--output", e.outputDir, fmt.Sprint(e.mrIID))
	for _, want := range []string{"1 replies", "edited 1", "resolved 1"} {
		if !strings.Contains(secondOut, want) {
			t.Fatalf("mutation push missing %q:\n%s", want, secondOut)
		}
	}
	if noOpOut := e.runCrit("push", "--forge", "gitlab", "--output", e.outputDir, fmt.Sprint(e.mrIID)); !strings.Contains(noOpOut, "No comments to push.") {
		t.Fatalf("repeated push was not a no-op:\n%s", noOpOut)
	}

	discussions := e.discussions()
	if len(discussions) != 1 || len(discussions[0].Notes) != 2 || !discussions[0].Notes[0].Resolved || !strings.Contains(discussions[0].Notes[0].Body, "edited GitLab comment") {
		t.Fatalf("remote discussion mismatch: %+v", discussions)
	}
	if pullOut := e.runCrit("pull", "--forge", "gitlab", "--output", e.outputDir, fmt.Sprint(e.mrIID)); !strings.Contains(pullOut, "No new inline comments") {
		t.Fatalf("repeated pull was not a no-op:\n%s", pullOut)
	}

	local = e.reviewFile()
	comment = local.Files["crit_gitlab_roundtrip.go"].Comments[0]
	e.editReviewFile(func(cj *CritJSON) {
		cj.Files["crit_gitlab_roundtrip.go"] = CritJSONFile{Status: "added"}
		cj.PendingRemoteDeletes = []RemoteRef{{
			Forge: "gitlab", ChangeNumber: e.mrIID,
			CommentID: comment.GitLabNoteID, ThreadID: comment.GitLabDiscussionID,
		}}
	})
	deleteOut := e.runCrit("push", "--forge", "gitlab", "--output", e.outputDir, fmt.Sprint(e.mrIID))
	if !strings.Contains(deleteOut, "deleted 1") || len(e.discussions()) != 0 {
		t.Fatalf("delete push mismatch:\n%s\nremote=%+v", deleteOut, e.discussions())
	}
}

func mustGitLabAPI(t *testing.T, host, endpoint, method string, payload any) []byte {
	t.Helper()
	out, err := runGitLabAPI(host, endpoint, method, payload)
	if err != nil {
		t.Fatalf("glab api %s: %v", endpoint, err)
	}
	return out
}

func tryGitLabAPI(host, endpoint, method string, payload any) {
	_, _ = runGitLabAPI(host, endpoint, method, payload)
}

func runGitLabAPI(host, endpoint, method string, payload any) ([]byte, error) {
	args := []string{"api"}
	if method != "" && method != "GET" {
		args = append(args, "--method", method)
	}
	if host != "" {
		args = append(args, "--hostname", host)
	}
	args = append(args, endpoint)
	var input []byte
	if payload != nil {
		var err error
		input, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		args = append(args, "--header", "Content-Type: application/json", "--input", "-")
	}
	cmd := exec.Command("glab", args...)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return out, nil
}

func mustRunGitLabRoundtrip(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed in %s: %v\n%s", name, args, dir, err, out)
	}
}
