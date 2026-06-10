package server_test

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/server"
)

func TestResolveDaemonCLIConfig_RemoteFlagIgnoredWithoutFocus(t *testing.T) {
	sc, err := server.ResolveDaemonCLIConfig([]string{"--remote", "file.md"})
	if err != nil {
		t.Fatal(err)
	}
	if sc.RemoteFiles {
		t.Errorf("expected RemoteFiles=false without --pr/--range, got %+v", sc)
	}
}

func TestResolveDaemonCLIConfig_RemoteDefaultsFalse(t *testing.T) {
	sc, err := server.ResolveDaemonCLIConfig([]string{"file.md"})
	if err != nil {
		t.Fatal(err)
	}
	if sc.RemoteFiles {
		t.Errorf("expected RemoteFiles=false by default, got %+v", sc)
	}
}

func TestFocusKeyArgs_PR(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{Kind: server.FocusRange, PRNumber: 295}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:295" {
		t.Errorf("got %v want [pr:295]", got)
	}
}

func TestFocusKeyArgs_Range(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{Kind: server.FocusRange, BaseSHA: "abc", HeadSHA: "def"}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "range:abc..def" {
		t.Errorf("got %v want [range:abc..def]", got)
	}
}

func TestFocusKeyArgs_FallsBackToFiles(t *testing.T) {
	sc := &server.DaemonCLIConfig{Files: []string{"a.md", "b.md"}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 2 || got[0] != "a.md" || got[1] != "b.md" {
		t.Errorf("got %v", got)
	}
}
