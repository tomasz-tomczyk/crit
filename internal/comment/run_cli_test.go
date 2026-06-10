package comment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
)

func TestRunComment_MissingArgs(t *testing.T) {
	err := RunComment([]string{})
	if err == nil {
		t.Fatal("expected usage error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
}

func TestRunComment_InvalidLocation(t *testing.T) {
	err := RunComment([]string{"noColonHere", "some body"})
	if err == nil {
		t.Fatal("expected error for invalid location")
	}
	if !strings.Contains(err.Error(), "invalid location") {
		t.Errorf("got %v", err)
	}
}

func TestRunComment_InvalidLineNumber(t *testing.T) {
	err := RunComment([]string{"file.go:abc", "some body"})
	if err == nil {
		t.Fatal("expected error for invalid line number")
	}
}

func TestRunComment_ReviewLevel(t *testing.T) {
	tmp := t.TempDir()
	if err := RunComment([]string{"--output", tmp, "--author", "TestBot", "overall looks good"}); err != nil {
		t.Fatalf("RunComment: %v", err)
	}
	cj, err := loadCritJSON(filepath.Join(tmp, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 || cj.ReviewComments[0].Body != "overall looks good" {
		t.Errorf("review comment not saved: %+v", cj.ReviewComments)
	}
}

func TestRunComment_FileLevel(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	path := filepath.Join(tmp, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunComment([]string{"--output", tmp, "--author", "Bot", "test.go", "file comment"}); err != nil {
		t.Fatalf("RunComment: %v", err)
	}
	cj, err := loadCritJSON(filepath.Join(tmp, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cj.Files["test.go"].Comments) != 1 {
		t.Fatalf("expected file comment, got %+v", cj.Files)
	}
}

func TestRunComment_LineLevel(t *testing.T) {
	tmp := t.TempDir()
	if err := RunComment([]string{"--output", tmp, "--author", "Bot", "main.go:42", "line comment"}); err != nil {
		t.Fatalf("RunComment: %v", err)
	}
	cj, err := loadCritJSON(filepath.Join(tmp, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	c := cj.Files["main.go"].Comments[0]
	if c.StartLine != 42 || c.EndLine != 42 {
		t.Errorf("lines = %d-%d, want 42-42", c.StartLine, c.EndLine)
	}
}

func TestRunComment_RangeLine(t *testing.T) {
	tmp := t.TempDir()
	if err := RunComment([]string{"--output", tmp, "--author", "Bot", "test.go:10-25", "range body"}); err != nil {
		t.Fatalf("RunComment: %v", err)
	}
	cj, err := loadCritJSON(filepath.Join(tmp, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	c := cj.Files["test.go"].Comments[0]
	if c.StartLine != 10 || c.EndLine != 25 {
		t.Errorf("lines = %d-%d, want 10-25", c.StartLine, c.EndLine)
	}
}

func TestRunComment_InvalidRange(t *testing.T) {
	err := RunComment([]string{"file.go:10-abc", "some body"})
	if err == nil {
		t.Fatal("expected error for invalid range")
	}
}

func TestRunComment_Clear(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".crit.json"), []byte(`{"files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunComment([]string{"--output", tmp, "--clear"}); err != nil {
		t.Fatalf("RunComment --clear: %v", err)
	}
}

func TestRunComment_ReplyMissingBody(t *testing.T) {
	err := RunComment([]string{"--reply-to", "c_abc"})
	if err == nil {
		t.Fatal("expected usage error")
	}
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
}

func TestRunComment_PlanAndOutputConflict(t *testing.T) {
	err := RunComment([]string{"--plan", "my-plan", "--output", "/tmp/x", "body"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Errorf("got %v", err)
	}
}

func TestRunCommentJSONScoped_CountsCommentsAndReplies(t *testing.T) {
	tmp := t.TempDir()
	// Seed a comment to reply to.
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{{ID: "c1", Body: "orig"}}},
		},
	}
	if err := saveCritJSON(filepath.Join(tmp, ".crit"), cj); err != nil {
		t.Fatal(err)
	}

	jsonPath := filepath.Join(tmp, "bulk.json")
	payload := `[{"file":"b.go","line":1,"body":"new"},{"reply_to":"c1","body":"reply"}]`
	if err := os.WriteFile(jsonPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RunComment([]string{"--json", "--file", jsonPath, "--output", tmp, "--author", "bot"}); err != nil {
		t.Fatalf("RunComment --json: %v", err)
	}
	loaded, err := loadCritJSON(filepath.Join(tmp, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Files["b.go"].Comments) != 1 {
		t.Errorf("expected line comment in b.go, got %+v", loaded.Files)
	}
	if len(loaded.Files["a.go"].Comments[0].Replies) != 1 {
		t.Errorf("expected reply on c1, got %+v", loaded.Files["a.go"].Comments)
	}
}
