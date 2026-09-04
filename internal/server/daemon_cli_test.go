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
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{Kind: server.FocusRange, Forge: "github", ChangeNumber: 295}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:295" {
		t.Errorf("got %v want [pr:295]", got)
	}
}

func TestFocusKeyArgs_PRWithRemoteBaseProject(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{
		Kind: server.FocusRange, Forge: "github", ChangeNumber: 1,
		RemoteBaseProject: "myorg/repo-b",
	}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:myorg/repo-b#1" {
		t.Errorf("got %v want [pr:myorg/repo-b#1]", got)
	}
}

func TestFocusKeyArgs_PRWithEnterpriseHost(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{
		Kind: server.FocusRange, Forge: "github", ChangeNumber: 9,
		RemoteBaseProject: "acme/app", RemoteHost: "github.example.com",
	}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:github.example.com/acme/app#9" {
		t.Errorf("got %v want [pr:github.example.com/acme/app#9]", got)
	}
}

func TestFocusKeyArgs_PRWithGitHubDotComHostOmitsHost(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{
		Kind: server.FocusRange, Forge: "github", ChangeNumber: 4,
		RemoteBaseProject: "o/r", RemoteHost: "github.com",
	}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:o/r#4" {
		t.Errorf("got %v want [pr:o/r#4]", got)
	}
}

func TestFocusKeyArgs_NilConfig(t *testing.T) {
	if got := server.FocusKeyArgs(nil); got != nil {
		t.Errorf("got %v want nil", got)
	}
}

func TestFocusKeyArgs_MR(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{Kind: server.FocusRange, Forge: "gitlab", ChangeNumber: 42}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "mr:42" {
		t.Errorf("got %v want [mr:42]", got)
	}
}

func TestFocusKeyArgs_MRWithRemoteBaseProject(t *testing.T) {
	sc := &server.DaemonCLIConfig{Focus: &server.Focus{
		Kind: server.FocusRange, Forge: "gitlab", ChangeNumber: 7,
		RemoteBaseProject: "acme/widget",
	}}
	got := server.FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "mr:acme/widget#7" {
		t.Errorf("got %v want [mr:acme/widget#7]", got)
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
