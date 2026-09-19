package browser

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLooksLikeWSL(t *testing.T) {
	t.Run("non-linux is false", func(t *testing.T) {
		if looksLikeWSL("darwin", "", "", "microsoft") {
			t.Fatal("expected false for non-linux runtime")
		}
	})

	t.Run("environment markers enable detection", func(t *testing.T) {
		if !looksLikeWSL("linux", "Ubuntu", "", "") {
			t.Fatal("expected WSL when WSL_DISTRO_NAME is set")
		}
		if !looksLikeWSL("linux", "", "/run/WSL/123_interop", "") {
			t.Fatal("expected WSL when WSL_INTEROP is set")
		}
	})

	t.Run("proc version marker enables detection", func(t *testing.T) {
		if !looksLikeWSL("linux", "", "", "Linux version 6.6.87.2-microsoft-standard-WSL2") {
			t.Fatal("expected WSL when /proc/version mentions microsoft")
		}
	})

	t.Run("plain linux is false", func(t *testing.T) {
		if looksLikeWSL("linux", "", "", "Linux version 6.8.0-generic") {
			t.Fatal("expected false for non-WSL linux")
		}
	})
}

func TestBrowserCommandSpecs(t *testing.T) {
	url := "http://localhost:1234?a=1&b=2"

	t.Run("wsl prefers windows-aware launchers before xdg-open", func(t *testing.T) {
		specs := browserCommandSpecs("linux", url, "", true, func(name string) bool {
			switch name {
			case "wslview", "cmd.exe", "powershell.exe", "xdg-open":
				return true
			default:
				return false
			}
		})

		want := []browserCommandSpec{
			{name: "wslview", args: []string{url}},
			{name: "powershell.exe", args: []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Process 'http://localhost:1234?a=1&b=2'"}},
			{name: "cmd.exe", args: []string{"/c", `start "" "http://localhost:1234?a=1&b=2"`}},
			{name: "xdg-open", args: []string{url}},
		}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("plain linux uses xdg-open", func(t *testing.T) {
		specs := browserCommandSpecs("linux", url, "", false, func(name string) bool {
			return name == "xdg-open"
		})

		want := []browserCommandSpec{{name: "xdg-open", args: []string{url}}}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("missing launcher returns no commands", func(t *testing.T) {
		specs := browserCommandSpecs("linux", url, "", false, func(string) bool { return false })
		if len(specs) != 0 {
			t.Fatalf("expected no commands, got %#v", specs)
		}
	})

	t.Run("darwin uses open", func(t *testing.T) {
		specs := browserCommandSpecs("darwin", url, "", false, func(string) bool { return false })
		want := []browserCommandSpec{{name: "open", args: []string{url}}}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("custom linux browser is tried before xdg-open", func(t *testing.T) {
		specs := browserCommandSpecs("linux", url, "/usr/local/bin/open-local", false, func(name string) bool {
			return name == "xdg-open"
		})

		want := []browserCommandSpec{
			{name: "/usr/local/bin/open-local", args: []string{url}},
			{name: "xdg-open", args: []string{url}},
		}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("custom mac command is executed directly", func(t *testing.T) {
		specs := browserCommandSpecs("darwin", url, "open-local", false, func(string) bool { return false })
		want := []browserCommandSpec{
			{name: "open-local", args: []string{url}},
			{name: "open", args: []string{url}},
		}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("custom mac script path is executed directly", func(t *testing.T) {
		specs := browserCommandSpecs("darwin", url, "/usr/local/bin/open-local", false, func(string) bool { return false })
		want := []browserCommandSpec{
			{name: "/usr/local/bin/open-local", args: []string{url}},
			{name: "open", args: []string{url}},
		}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})
}

func TestCommandQuoting(t *testing.T) {
	t.Run("powershell quotes literal strings", func(t *testing.T) {
		got := powershellSingleQuote("https://example.com?a=1&b=2'3")
		want := "'https://example.com?a=1&b=2''3'"
		if got != want {
			t.Fatalf("powershellSingleQuote() = %q, want %q", got, want)
		}
	})

	t.Run("cmd double-quotes strings", func(t *testing.T) {
		got := cmdDoubleQuote(`https://example.com?a=1&b=2"3`)
		want := `"https://example.com?a=1&b=2""3"`
		if got != want {
			t.Fatalf("cmdDoubleQuote() = %q, want %q", got, want)
		}
	})
}

func TestCommandExists(t *testing.T) {
	t.Run("finds executable on PATH", func(t *testing.T) {
		binDir := t.TempDir()
		// Windows LookPath only finds names matching PATHEXT (.exe/.cmd/…).
		fake := filepath.Join(binDir, "crit-test-exe")
		content := []byte("#!/bin/sh\n")
		if runtime.GOOS == "windows" {
			fake += ".cmd"
			content = []byte("@echo off\r\n")
		}
		if err := os.WriteFile(fake, content, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		if !commandExists("crit-test-exe") {
			t.Error("expected commandExists to find fake executable")
		}
	})

	t.Run("missing executable returns false", func(t *testing.T) {
		if commandExists("crit-test-definitely-missing-" + strconv.Itoa(int(time.Now().UnixNano()))) {
			t.Error("expected commandExists to return false for missing command")
		}
	})
}

func TestSystemIsWSL(t *testing.T) {
	t.Run("non-linux returns false", func(t *testing.T) {
		if runtime.GOOS != "linux" && systemIsWSL() {
			t.Error("expected false on non-linux OS")
		}
	})

	t.Run("linux with WSL env marker", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("linux-only")
		}
		t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
		t.Cleanup(func() { os.Unsetenv("WSL_DISTRO_NAME") })
		if !systemIsWSL() {
			t.Error("expected WSL when WSL_DISTRO_NAME is set")
		}
	})
}

func TestOpenBrowserWithCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake browser shim is POSIX-only")
	}

	t.Run("custom command success does not print warning", func(t *testing.T) {
		binDir := t.TempDir()
		fake := filepath.Join(binDir, "crit-test-browser")
		script := "#!/bin/sh\nexit 0\n"
		if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		stderr := captureStderr(t, func() {
			OpenBrowserWithCommand("http://example.test", "crit-test-browser")
		})
		if strings.Contains(stderr, "could not open browser") {
			t.Errorf("unexpected warning on stderr: %q", stderr)
		}
	})
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestTryOpenBrowser(t *testing.T) {
	t.Run("falls back until a launcher succeeds", func(t *testing.T) {
		specs := []browserCommandSpec{
			{name: "wslview", args: []string{"http://localhost:1234"}},
			{name: "powershell.exe", args: []string{"-Command", "Start-Process", "http://localhost:1234"}},
			{name: "cmd.exe", args: []string{"/c", "start", "", "http://localhost:1234"}},
		}
		var attempted []string
		ok := tryOpenBrowser(specs, func(spec browserCommandSpec) error {
			attempted = append(attempted, spec.name)
			if spec.name == "cmd.exe" {
				return nil
			}
			return assertAnError{}
		})
		if !ok {
			t.Fatal("expected fallback chain to succeed")
		}
		want := []string{"wslview", "powershell.exe", "cmd.exe"}
		if !reflect.DeepEqual(attempted, want) {
			t.Fatalf("attempted = %#v, want %#v", attempted, want)
		}
	})

	t.Run("returns false when all launchers fail", func(t *testing.T) {
		specs := []browserCommandSpec{
			{name: "wslview"},
			{name: "powershell.exe"},
		}
		ok := tryOpenBrowser(specs, func(browserCommandSpec) error {
			return assertAnError{}
		})
		if ok {
			t.Fatal("expected failure when every launcher errors")
		}
	})
}

type assertAnError struct{}

func (assertAnError) Error() string {
	return "boom"
}
