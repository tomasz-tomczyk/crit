package main

import (
	"reflect"
	"testing"
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
		specs := browserCommandSpecs("linux", url, true, func(name string) bool {
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
		specs := browserCommandSpecs("linux", url, false, func(name string) bool {
			return name == "xdg-open"
		})

		want := []browserCommandSpec{{name: "xdg-open", args: []string{url}}}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("browserCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("missing launcher returns no commands", func(t *testing.T) {
		specs := browserCommandSpecs("linux", url, false, func(string) bool { return false })
		if len(specs) != 0 {
			t.Fatalf("expected no commands, got %#v", specs)
		}
	})

	t.Run("darwin uses open", func(t *testing.T) {
		specs := browserCommandSpecs("darwin", url, false, func(string) bool { return false })
		want := []browserCommandSpec{{name: "open", args: []string{url}}}
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
