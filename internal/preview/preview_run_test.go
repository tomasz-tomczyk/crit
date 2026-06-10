package preview

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConnectToPreviewDaemon_NoDaemon(t *testing.T) {
	if connectToPreviewDaemonForTest("nonexistent-key-12345", true) {
		t.Error("expected false when no daemon running")
	}
}

func TestRunPreview_NoFileExits(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunPreviewNoFile", "--")
		cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected non-zero exit when no file given")
		}
		return
	}
	RunPreview([]string{"--no-open"})
}

func TestHelperProcess_RunPreviewNoFile(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	RunPreview([]string{"--no-open"})
}

func TestRunPreview_MissingFileExits(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunPreviewMissingFile", "--")
		cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected non-zero exit for missing file")
		}
		return
	}
	RunPreview([]string{"/nonexistent/missing.html", "--no-open"})
}

func TestHelperProcess_RunPreviewMissingFile(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	RunPreview([]string{"/nonexistent/missing.html", "--no-open"})
}

func TestRunPreview_FlagParsing(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "page.html")
	if err := os.WriteFile(html, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without a running daemon this will try to start one and block — only
	// verify LooksLikePreviewArgs + key generation path used by RunPreview.
	if !LooksLikePreviewArgs([]string{html}) {
		t.Fatal("expected preview args")
	}
	key := PreviewSessionKey(dir, html)
	if len(key) != 12 {
		t.Errorf("key len = %d", len(key))
	}
}
