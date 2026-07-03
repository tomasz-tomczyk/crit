package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

func TestSyncCommentsFromDisk_ClearsPendingWrite(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(filePath, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	critPath := filepath.Join(dir, ".crit")
	if err := review.SaveCritJSON(critPath, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		Mode:      "files",
		OutputDir: dir,
		RepoRoot:  dir,
		Files:     []*FileEntry{{Path: "plan.md", AbsPath: filePath}},
	}
	// Mirrors share flow: scheduling a debounced write blocks mergeExternalCritJSON.
	sess.SetSharedURLAndToken("https://crit.md/r/tok", "del")
	if sess.PendingWriteForTest() != true {
		t.Fatal("expected pendingWrite after SetSharedURLAndToken")
	}

	if err := share.MergeWebComments(critPath, []share.WebComment{{
		Body: "remote", FilePath: "plan.md", StartLine: 1, EndLine: 1,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	if !sess.SyncCommentsFromDisk() {
		t.Fatal("SyncCommentsFromDisk returned false with pendingWrite set")
	}
	if len(sess.GetComments("plan.md")) != 1 {
		t.Fatalf("want 1 comment, got %d", len(sess.GetComments("plan.md")))
	}
}

func TestSyncCommentsFromDisk_AfterMergeWebComments_WithNewServer(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(filePath, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		Mode:        "files",
		OutputDir:   dir,
		RepoRoot:    dir,
		ReviewRound: 1,
		Files:       []*FileEntry{{Path: "plan.md", AbsPath: filePath}},
	}
	writeCritJSONForTest(t, dir, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{}},
		},
	})
	if info, err := os.Stat(review.ReviewPathsFor(sess.CritJSONPath()).Review); err != nil {
		t.Fatal(err)
	} else {
		sess.SetLastCritJSONMtimeForTest(info.ModTime())
	}

	if _, err := NewServer(sess, frontendFS, "", false, "", "", "test", 0, ""); err != nil {
		t.Fatal(err)
	}

	critPath := sess.CritJSONPath()
	if err := share.MergeWebComments(critPath, []share.WebComment{{
		Body:              "new web comment",
		FilePath:          "plan.md",
		StartLine:         1,
		EndLine:           1,
		ExternalID:        "ext-1",
		AuthorDisplayName: "Web User",
	}}, nil); err != nil {
		t.Fatal(err)
	}

	if !sess.SyncCommentsFromDisk() {
		t.Fatalf("SyncCommentsFromDisk returned false (pendingWrite=%v)", sess.PendingWriteForTest())
	}
	if len(sess.GetComments("plan.md")) != 1 {
		t.Fatalf("want 1 visible comment")
	}
}

func TestSyncCommentsFromDisk_AfterMergeWebComments(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(filePath, []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	critPath := filepath.Join(dir, ".crit")
	if err := review.SaveCritJSON(critPath, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sess := &Session{
		Mode:      "files",
		OutputDir: dir,
		RepoRoot:  dir,
		Files:     []*FileEntry{{Path: "plan.md", AbsPath: filePath}},
	}
	if info, err := os.Stat(review.ReviewPathsFor(critPath).Review); err != nil {
		t.Fatal(err)
	} else {
		sess.SetLastCritJSONMtimeForTest(info.ModTime())
	}

	if err := share.MergeWebComments(critPath, []share.WebComment{{
		Body:              "new web comment",
		FilePath:          "plan.md",
		StartLine:         1,
		EndLine:           1,
		ExternalID:        "ext-1",
		AuthorDisplayName: "Web User",
	}}, nil); err != nil {
		t.Fatal(err)
	}

	if !sess.SyncCommentsFromDisk() {
		t.Fatal("SyncCommentsFromDisk returned false")
	}

	sess.RLock()
	raw := len(sess.Files[0].Comments)
	focus := sess.Focus
	sess.RUnlock()
	if raw != 1 {
		t.Fatalf("in-memory file has %d comments, want 1", raw)
	}

	visible := sess.GetComments("plan.md")
	if len(visible) != 1 {
		t.Fatalf("GetComments returned %d, want 1 (raw=%d focus=%+v)", len(visible), raw, focus)
	}
}
