package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/comment"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestPrintHelpMentionsSession(t *testing.T) {
	var stderr strings.Builder
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		io.Copy(&stderr, r)
		close(done)
	}()
	printHelp()
	w.Close()
	<-done
	os.Stderr = old
	out := stderr.String()
	if !strings.Contains(out, "--session") {
		t.Fatalf("help missing --session:\n%s", out)
	}
	if !strings.Contains(out, "--public-url") {
		t.Fatalf("help missing --public-url:\n%s", out)
	}
	if !strings.Contains(out, "CRIT_PUBLIC_URL") {
		t.Fatalf("help missing CRIT_PUBLIC_URL:\n%s", out)
	}
}

// TestSubcommandDispatch_Help verifies that help flags are recognized.
func TestSubcommandDispatch_Help(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Help", "--")
			cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "GO_TEST_HELP_ARG="+arg)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("help %q exited with error: %v\noutput: %s", arg, err, out)
			}
		})
	}
}

func TestHelperProcess_Help(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	arg := os.Getenv("GO_TEST_HELP_ARG")
	os.Args = []string{"crit", arg}
	var stderr strings.Builder
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		io.Copy(&stderr, r)
		close(done)
	}()
	printHelp()
	w.Close()
	<-done
	os.Stderr = old
	if !strings.Contains(stderr.String(), "--session") {
		t.Fatalf("help missing --session:\n%s", stderr.String())
	}
}

// TestSubcommandDispatch_Version verifies the version flag.
func TestSubcommandDispatch_Version(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Version", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version exited with error: %v\noutput: %s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected version output, got empty")
	}
}

func TestHelperProcess_Version(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	printVersion()
}

// TestSubcommandDispatch_Config verifies that "crit config --generate" produces output.
func TestSubcommandDispatch_Config(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Config", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config --generate exited with error: %v\noutput: %s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("expected config output, got empty")
	}
}

func TestHelperProcess_Config(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runConfig([]string{"--generate"})
}

// TestInstallGeminiSettings_MalformedJSON verifies that a malformed settings.json
// causes a non-zero exit instead of silently overwriting user data.
func TestInstallGeminiSettings_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("not json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_GeminiSettingsBadJSON", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "GO_TEST_SETTINGS_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for malformed settings.json")
	}
	if !strings.Contains(string(out), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' in stderr, got: %s", out)
	}
}

func TestHelperProcess_GeminiSettingsBadJSON(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	installGeminiSettings(os.Getenv("GO_TEST_SETTINGS_PATH"), false)
}

// TestRunComment_MissingArgs verifies that runComment exits with usage when given no args.
func TestRunComment_MissingArgs(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing comment args")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T", err)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatal("expected non-zero exit code")
	}
}

func TestHelperProcess_CommentMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{})
}

// TestRunComment_InvalidLocation verifies that a bad location format exits with error.
func TestRunComment_InvalidLocation(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadLoc", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid location")
	}
}

func TestHelperProcess_CommentBadLoc(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	// No colon in location
	runComment([]string{"noColonHere", "some body"})
}

// TestRunComment_InvalidLineNumber verifies that a non-numeric line exits with error.
func TestRunComment_InvalidLineNumber(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadLine", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid line number")
	}
}

func TestHelperProcess_CommentBadLine(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{"file.go:abc", "some body"})
}

// TestRunInstall_MissingAgent verifies that runInstall with no args exits with usage.
func TestRunInstall_MissingAgent(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_InstallMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing install agent")
	}
}

func TestHelperProcess_InstallMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runInstall([]string{})
}

// TestRunShare_MissingFiles verifies that runShare with no files exits with usage.
func TestRunShare_MissingFiles(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ShareMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for missing share files")
	}
}

func TestHelperProcess_ShareMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runShare([]string{})
}

// TestRunComment_FlagParsing verifies that --output and --author flags are parsed correctly.
func TestRunComment_FlagParsing(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentFlags", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment with flags exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentFlags(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	// Write a dummy file so the comment can reference it
	os.WriteFile(tmp+"/test.go", []byte("package main\n"), 0o644)
	runComment([]string{"--output", tmp, "--author", "TestBot", "test.go:1", "test body"})
}

// TestRunComment_ClearFlag verifies that --clear works.
func TestRunComment_ClearFlag(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentClear", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment --clear exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentClear(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	// Write a .crit.json to clear
	os.WriteFile(tmp+"/.crit.json", []byte(`{"files":{}}`), 0o644)
	runComment([]string{"--output", tmp, "--clear"})
}

// TestRunComment_RangeLine verifies that a range line spec like "10-25" is parsed.
func TestRunComment_RangeLine(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentRange", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("comment with range exited with error: %v\noutput: %s", err, out)
	}
}

func TestHelperProcess_CommentRange(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	runComment([]string{"--output", tmp, "--author", "Bot", "test.go:10-25", "range body"})
}

// TestRunComment_InvalidRange verifies that a bad range like "10-abc" exits with error.
func TestRunComment_InvalidRange(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CommentBadRange", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid range")
	}
}

func TestHelperProcess_CommentBadRange(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runComment([]string{"file.go:10-abc", "some body"})
}

// TestRunShare_OutputFlagMissingValue verifies that --output without value exits with error.
func TestRunShare_OutputFlagMissingValue(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ShareOutputMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for --output without value")
	}
}

func TestHelperProcess_ShareOutputMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runShare([]string{"--output"})
}

// TestRunShare_ConsentDenied verifies that answering "n" to the first-time
// consent prompt exits cleanly without sharing.
func TestRunShare_ConsentDenied(t *testing.T) {
	home := t.TempDir()
	outDir := t.TempDir()
	f := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(f, []byte("# Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_ShareConsentDenied", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "HOME="+home,
		"GO_TEST_SHARE_FILE="+f, "GO_TEST_SHARE_OUT="+outDir)
	cmd.Stdin = strings.NewReader("n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected zero exit when user declines consent, got: %v\n%s", err, out)
	}
}

func TestHelperProcess_ShareConsentDenied(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runShare([]string{"--output", os.Getenv("GO_TEST_SHARE_OUT"), os.Getenv("GO_TEST_SHARE_FILE")})
}

// TestRunUnpublish_OutputFlagMissingValue verifies that --output without value exits with error.
func TestRunUnpublish_OutputFlagMissingValue(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_UnpublishOutputMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for --output without value")
	}
}

func TestHelperProcess_UnpublishOutputMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runUnpublish([]string{"--output"})
}

// TestRunComment_JSONFlag verifies that --json reads from stdin and produces output.
func TestRunComment_JSONFlag(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_CommentJSON$", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	cmd.Stdin = strings.NewReader(`[{"file":"main.go","line":1,"body":"test"}]`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Added 1 comment") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestHelperProcess_CommentJSON(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	runComment([]string{"--json", "--output", tmp, "--author", "TestBot"})
}

// TestRunComment_JSONFlagMixed verifies that --json handles mixed comments and replies.
func TestRunComment_JSONFlagMixed(t *testing.T) {
	// Step 1: Create a comment and capture its ID
	tmp := t.TempDir()
	err := comment.AddCommentToCritJSONScoped("main.go", 1, 1, "comment", "TestBot", "", tmp, focus.InheritedScope{})
	if err != nil {
		t.Fatalf("setup comment: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".crit", "review.json"))
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj session.CritJSON
	json.Unmarshal(data, &cj)
	commentID := cj.Files["main.go"].Comments[0].ID

	// Step 2: Run --json with a new comment + reply to the existing comment
	input := fmt.Sprintf(`[{"file":"main.go","line":5,"body":"another"},{"reply_to":%q,"body":"reply","resolve":true}]`, commentID)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_CommentJSONMix$", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "TEST_OUTPUT_DIR="+tmp)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("process exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "1 comment") || !strings.Contains(string(out), "1 reply") {
		t.Errorf("expected mixed success message, got: %s", out)
	}
}

func TestHelperProcess_CommentJSONMix(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := os.Getenv("TEST_OUTPUT_DIR")
	if tmp == "" {
		tmp = t.TempDir()
	}
	runComment([]string{"--json", "--output", tmp, "--author", "TestBot"})
}

// TestFetch_PrintsReviewFilePath verifies that crit fetch prints the review
// file path in both the "no new comments" and "fetched N comments" cases.
func TestFetch_PrintsReviewFilePath(t *testing.T) {
	tests := []struct {
		name        string
		comments    []share.WebComment
		wantContain string
	}{
		{
			name:        "no new comments",
			comments:    nil,
			wantContain: "No new comments.",
		},
		{
			name: "with new comments",
			comments: []share.WebComment{
				{Body: "fix this", FilePath: "main.go", StartLine: 10, EndLine: 10, Scope: "line"},
			},
			wantContain: "Fetched 1 new comment(s)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tc.comments)
			}))
			defer ts.Close()

			tmpDir := t.TempDir()
			cj := session.CritJSON{
				ShareURL: ts.URL + "/r/test123",
				Files:    map[string]session.CritJSONFile{},
			}
			data, err := json.Marshal(cj)
			if err != nil {
				t.Fatal(err)
			}
			critPath := filepath.Join(tmpDir, ".crit")
			if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_Fetch", "--")
			cmd.Env = append(os.Environ(),
				"GO_TEST_HELPER=1",
				"GO_TEST_FETCH_OUTPUT_DIR="+tmpDir,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fetch exited with error: %v\noutput: %s", err, out)
			}
			output := string(out)

			if !strings.Contains(output, tc.wantContain) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.wantContain, output)
			}
			wantPath := "Review file: " + critPath
			if !strings.Contains(output, wantPath) {
				t.Errorf("expected output to contain %q, got:\n%s", wantPath, output)
			}
		})
	}
}

func TestHelperProcess_Fetch(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	outputDir := os.Getenv("GO_TEST_FETCH_OUTPUT_DIR")
	runFetch([]string{"--output", outputDir})
}
