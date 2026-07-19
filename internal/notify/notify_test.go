package notify

import (
	"reflect"
	"strings"
	"testing"
)

func TestDesktopCommandSpecs(t *testing.T) {
	title := "Crit"
	body := "Round 2 is ready for review"
	url := "http://127.0.0.1:3456"

	t.Run("darwin uses osascript", func(t *testing.T) {
		specs := desktopCommandSpecs("darwin", title, body, url, func(string) bool { return true })
		if len(specs) != 1 {
			t.Fatalf("got %d specs, want 1", len(specs))
		}
		if specs[0].name != "osascript" {
			t.Fatalf("name = %q, want osascript", specs[0].name)
		}
		joined := strings.Join(specs[0].args, " ")
		for _, want := range []string{title, body, url} {
			if !strings.Contains(joined, want) {
				t.Fatalf("osascript args missing %q: %q", want, joined)
			}
		}
	})

	t.Run("linux prefers notify-send then zenity", func(t *testing.T) {
		specs := desktopCommandSpecs("linux", title, body, url, func(name string) bool {
			return name == "notify-send" || name == "zenity"
		})
		want := []commandSpec{
			{name: "notify-send", args: []string{"--app-name=crit", title, body}},
			{name: "zenity", args: []string{"--notification", "--text=" + title + ": " + body}},
		}
		if !reflect.DeepEqual(specs, want) {
			t.Fatalf("desktopCommandSpecs() = %#v, want %#v", specs, want)
		}
	})

	t.Run("linux skips missing launchers", func(t *testing.T) {
		specs := desktopCommandSpecs("linux", title, body, url, func(string) bool { return false })
		if len(specs) != 0 {
			t.Fatalf("expected no specs, got %#v", specs)
		}
	})

	t.Run("windows uses powershell toast", func(t *testing.T) {
		specs := desktopCommandSpecs("windows", title, body, url, func(string) bool { return true })
		if len(specs) != 1 || specs[0].name != "powershell" {
			t.Fatalf("got %#v", specs)
		}
		script := specs[0].args[len(specs[0].args)-1]
		for _, want := range []string{title, body} {
			if !strings.Contains(script, want) {
				t.Fatalf("powershell script missing %q: %q", want, script)
			}
		}
	})
}

func TestTryDesktopStopsOnFirstSuccess(t *testing.T) {
	calls := 0
	ok := tryDesktop([]commandSpec{
		{name: "first", args: []string{"a"}},
		{name: "second", args: []string{"b"}},
	}, func(spec commandSpec) error {
		calls++
		if spec.name == "first" {
			return nil
		}
		t.Fatalf("should not run after success")
		return nil
	})
	if !ok {
		t.Fatal("expected success")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRoundReadyMessage(t *testing.T) {
	got := roundReadyBody(2, "http://127.0.0.1:9")
	if !strings.Contains(got, "Round 2") {
		t.Fatalf("body missing round: %q", got)
	}
	if !strings.Contains(got, "http://127.0.0.1:9") {
		t.Fatalf("body missing url: %q", got)
	}
}
