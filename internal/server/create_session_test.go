package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func chdirRepo(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestCreateSession_LoadsShareFromReviewPath(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.GitRun(t, dir, "checkout", "-b", "feat-share")
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n\nContent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vcs.GitRun(t, dir, "add", "new.md")
	vcs.GitRun(t, dir, "commit", "-m", "add new.md")

	reviewDir := filepath.Join(t.TempDir(), "feat-share-review")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reviewPath := filepath.Join(reviewDir, "review.json")
	cj := CritJSON{
		Branch:      "feat-share",
		BaseRef:     "abc",
		UpdatedAt:   time.Now().Format(time.RFC3339),
		ReviewRound: 2,
		ShareURL:    "https://crit.example.com/review/shared",
		DeleteToken: "tok_shared",
		ShareScope:  share.ShareScope([]string{"new.md"}),
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	chdirRepo(t, dir)
	sess, err := CreateSession(&DaemonCLIConfig{ReviewPath: reviewDir})
	if err != nil {
		t.Fatal(err)
	}
	if sess.ReviewFilePath != reviewDir {
		t.Errorf("ReviewFilePath = %q, want %q", sess.ReviewFilePath, reviewDir)
	}
	url, token := sess.GetShareState()
	if url != "https://crit.example.com/review/shared" {
		t.Errorf("sharedURL = %q", url)
	}
	if token != "tok_shared" {
		t.Errorf("deleteToken = %q", token)
	}
	if sess.ReviewRound != 2 {
		t.Errorf("ReviewRound = %d, want 2", sess.ReviewRound)
	}
}

func TestCreateSession_RangeFocus_CleanWorkingTree(t *testing.T) {
	tests := []struct {
		name           string
		featureBranch  bool
		wantBranch     string
		wantBaseBranch string
	}{
		{name: "default branch, clean tree", featureBranch: false, wantBranch: "main", wantBaseBranch: "main"},
		{name: "feature branch, clean tree", featureBranch: true, wantBranch: "feature/clean-tree", wantBaseBranch: "main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := vcs.InitTestRepo(t)
			baseSHA := vcs.GitRun(t, dir, "rev-parse", "HEAD")
			if tc.featureBranch {
				vcs.GitRun(t, dir, "checkout", "-b", "feature/clean-tree")
			}
			if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			vcs.GitRun(t, dir, "add", "a.md")
			vcs.GitRun(t, dir, "commit", "-m", "add a")
			headSHA := vcs.GitRun(t, dir, "rev-parse", "HEAD")

			chdirRepo(t, dir)
			sess, err := CreateSession(&DaemonCLIConfig{
				Focus: &Focus{Kind: FocusRange, BaseSHA: baseSHA, HeadSHA: headSHA},
			})
			if err != nil {
				t.Fatalf("createSession with range focus on clean working tree: %v", err)
			}
			ApplySessionOverrides(sess, &DaemonCLIConfig{
				Focus: &Focus{Kind: FocusRange, BaseSHA: baseSHA, HeadSHA: headSHA},
			})
			if sess.Mode != "git" {
				t.Errorf("Mode = %q, want git", sess.Mode)
			}
			if sess.Branch != tc.wantBranch {
				t.Errorf("Branch = %q, want %q", sess.Branch, tc.wantBranch)
			}
			if sess.BaseBranchName != tc.wantBaseBranch {
				t.Errorf("BaseBranchName = %q, want %q", sess.BaseBranchName, tc.wantBaseBranch)
			}
		})
	}
}

func TestCreateSession_NoFocus_CleanWorkingTree(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	vcs.GitRun(t, dir, "checkout", "-b", "feature/no-focus-clean")
	chdirRepo(t, dir)

	_, err := CreateSession(&DaemonCLIConfig{})
	if !errors.Is(err, session.ErrNoChangedFiles) {
		t.Fatalf("err = %v, want ErrNoChangedFiles", err)
	}
}

func TestCreateSession_FilesMode_LoadsShareFromReviewPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TODO(windows): 8.3 short-name vs long-name path mismatch on GH Actions runner")
	}
	dir := vcs.InitTestRepo(t)
	mdPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(mdPath, []byte("# Doc\n\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirRepo(t, dir)

	scope := share.ShareScope([]string{"doc.md"})
	reviewDir := filepath.Join(t.TempDir(), "files-review")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		ShareURL:    "https://crit.example.com/review/files",
		DeleteToken: "tok_files",
		ShareScope:  scope,
		ReviewRound: 3,
		Files:       map[string]CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reviewDir, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := CreateSession(&DaemonCLIConfig{
		Files:      []string{"doc.md"},
		ReviewPath: reviewDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	url, token := sess.GetShareState()
	if url != "https://crit.example.com/review/files" {
		t.Errorf("sharedURL = %q", url)
	}
	if token != "tok_files" {
		t.Errorf("deleteToken = %q", token)
	}
	if sess.ReviewRound != 3 {
		t.Errorf("ReviewRound = %d, want 3", sess.ReviewRound)
	}
}

func TestApplySessionOverrides_OutputDirSkipsPlan(t *testing.T) {
	dir := vcs.InitTestRepo(t)
	chdirRepo(t, dir)
	sess, err := CreateSession(&DaemonCLIConfig{Files: []string{"README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	beforeMode := sess.Mode
	ApplySessionOverrides(sess, &DaemonCLIConfig{
		PlanDir:   filepath.Join(t.TempDir(), "plans", "auth"),
		PlanName:  "auth",
		OutputDir: t.TempDir(),
	})
	if sess.Mode != beforeMode {
		t.Fatalf("OutputDir should skip plan overrides; Mode changed %q → %q", beforeMode, sess.Mode)
	}
	if sess.OutputDir != "" {
		t.Fatalf("OutputDir override must not set sess.OutputDir (legacy layout), got %q", sess.OutputDir)
	}
}

func TestApplySessionOverrides_PlanRestartKeepsSavedCommentEdits(t *testing.T) {
	const slug = "saved-plan"
	planDir := t.TempDir()
	planPath := filepath.Join(planDir, "current.md")
	if err := os.WriteFile(planPath, []byte("# Saved plan\n\nA step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reviewDir := filepath.Join(planDir, ".crit")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	saved := CritJSON{Files: map[string]CritJSONFile{
		slug + ".md": {Status: "modified", Comments: []Comment{{ID: "c1", StartLine: 3, EndLine: 3, Body: "Check this"}}},
	}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reviewDir, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sc := &DaemonCLIConfig{Files: []string{planPath}, ReviewPath: reviewDir, PlanDir: planDir, PlanName: slug}
	sess, err := CreateSession(sc)
	if err != nil {
		t.Fatal(err)
	}
	ApplySessionOverrides(sess, sc)
	if len(sess.Files) != 1 {
		t.Fatalf("plan restart has %d files, want 1: %+v", len(sess.Files), sess.Files)
	}
	if sess.Files[0].Orphaned || sess.Files[0].Path != slug+".md" || len(sess.Files[0].Comments) != 1 {
		t.Fatalf("plan file after restart = %+v", sess.Files[0])
	}

	if _, ok := sess.AddReply(slug+".md", "c1", "A reply", "reviewer", ""); !ok {
		t.Fatal("AddReply failed")
	}
	if _, ok := sess.SetCommentResolved(slug+".md", "c1", true); !ok {
		t.Fatal("SetCommentResolved failed")
	}
	if err := sess.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(reviewDir, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written CritJSON
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	file := written.Files[slug+".md"]
	if file.Status == "removed" || len(file.Comments) != 1 || !file.Comments[0].Resolved || len(file.Comments[0].Replies) != 1 {
		t.Fatalf("saved plan comment edits lost: %+v", file)
	}

	restarted, err := CreateSession(sc)
	if err != nil {
		t.Fatal(err)
	}
	ApplySessionOverrides(restarted, sc)
	if len(restarted.Files) != 1 || len(restarted.Files[0].Comments) != 1 ||
		!restarted.Files[0].Comments[0].Resolved || len(restarted.Files[0].Comments[0].Replies) != 1 {
		t.Fatalf("saved plan comment edits not restored after restart: %+v", restarted.Files)
	}
}
