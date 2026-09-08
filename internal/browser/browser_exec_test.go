package browser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunBrowserCommandPassesURLAsLiteralArgument(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "browser-argument")
	t.Setenv("CRIT_BROWSER_TEST_MARKER", marker)
	url := `https://example.test/review?a=1&b=$(not-a-command)&q='quoted'`

	err := runBrowserCommand(browserCommandSpec{
		name: os.Args[0],
		args: []string{"-test.run=^TestBrowserCommandHelper$", "--", url},
	})
	if err != nil {
		t.Fatalf("runBrowserCommand: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read helper marker: %v", err)
	}
	if got := string(data); got != url {
		t.Fatalf("browser argument = %q, want %q", got, url)
	}
}

func TestRunBrowserCommandReturnsStartError(t *testing.T) {
	err := runBrowserCommand(browserCommandSpec{name: filepath.Join(t.TempDir(), "missing-browser")})
	if err == nil {
		t.Fatal("expected missing browser command to return an error")
	}
}

func TestBrowserCommandHelper(t *testing.T) {
	marker := os.Getenv("CRIT_BROWSER_TEST_MARKER")
	if marker == "" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+2 != len(os.Args) {
		t.Fatalf("helper args = %q, want exactly one argument after --", os.Args)
	}
	if err := os.WriteFile(marker, []byte(os.Args[separator+1]), 0o600); err != nil {
		t.Fatal(err)
	}
}
