package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestFocusKeyArgs_PR(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 295}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:295" {
		t.Errorf("got %v want [pr:295]", got)
	}
}

func TestFocusKeyArgs_PRWithRemoteBaseProject(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{
		Kind: FocusRange, Forge: "github", ChangeNumber: 1,
		RemoteBaseProject: "myorg/repo-b",
	}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:myorg/repo-b#1" {
		t.Errorf("got %v want [pr:myorg/repo-b#1]", got)
	}
}

func TestFocusKeyArgs_PRWithEnterpriseHost(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{
		Kind: FocusRange, Forge: "github", ChangeNumber: 9,
		RemoteBaseProject: "acme/app", RemoteHost: "github.example.com",
	}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:github.example.com/acme/app#9" {
		t.Errorf("got %v want [pr:github.example.com/acme/app#9]", got)
	}
}

func TestFocusKeyArgs_PRWithGitHubDotComHostOmitsHost(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{
		Kind: FocusRange, Forge: "github", ChangeNumber: 4,
		RemoteBaseProject: "o/r", RemoteHost: "github.com",
	}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:o/r#4" {
		t.Errorf("got %v want [pr:o/r#4]", got)
	}
}

func TestFocusKeyArgs_NilConfig(t *testing.T) {
	if got := FocusKeyArgs(nil); got != nil {
		t.Errorf("got %v want nil", got)
	}
}

func TestFocusKeyArgs_MR(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 42}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "mr:42" {
		t.Errorf("got %v want [mr:42]", got)
	}
}

func TestFocusKeyArgs_MRWithRemoteBaseProject(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{
		Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7,
		RemoteBaseProject: "acme/widget",
	}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "mr:acme/widget#7" {
		t.Errorf("got %v want [mr:acme/widget#7]", got)
	}
}

func TestFocusKeyArgs_MRWithSelfManagedHost(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{
		Kind: FocusRange, Forge: "gitlab", ChangeNumber: 9,
		RemoteBaseProject: "group/app", RemoteHost: "gitlab.example.com",
	}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "mr:gitlab.example.com/group/app#9" {
		t.Errorf("got %v want [mr:gitlab.example.com/group/app#9]", got)
	}
}

func TestPRFocusKey_MatchesFocusKeyFor(t *testing.T) {
	f := Focus{
		Kind: FocusRange, Forge: "github", ChangeNumber: 3,
		RemoteBaseProject: "o/r", RemoteHost: "github.example.com",
	}
	if got, want := focusKeyFor(f), PRFocusKey(3, "o/r", "github.example.com"); got != want {
		t.Fatalf("focusKeyFor=%q PRFocusKey=%q", got, want)
	}
}

func TestMRFocusKey_MatchesFocusKeyFor(t *testing.T) {
	f := Focus{
		Kind: FocusRange, Forge: "gitlab", ChangeNumber: 3,
		RemoteBaseProject: "g/p", RemoteHost: "gitlab.example.com",
	}
	if got, want := focusKeyFor(f), MRFocusKey(3, "g/p", "gitlab.example.com"); got != want {
		t.Fatalf("focusKeyFor=%q MRFocusKey=%q", got, want)
	}
}

func TestFocusKeyArgs_Range(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{Kind: FocusRange, BaseSHA: "abc", HeadSHA: "def"}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "range:abc..def" {
		t.Errorf("got %v want [range:abc..def]", got)
	}
}

func TestFetchSessionFocus(t *testing.T) {
	rangeFocus := &Focus{Kind: FocusRange, BaseSHA: "abc", HeadSHA: "def"}

	t.Run("returns focus from daemon", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"focus": rangeFocus})
		}))
		defer srv.Close()
		port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])
		got := fetchSessionFocus(&http.Client{}, "", port)
		if got == nil || got.Kind != FocusRange {
			t.Fatalf("got %+v want range focus", got)
		}
	})

	t.Run("returns nil on error", func(t *testing.T) {
		got := fetchSessionFocus(&http.Client{}, "", 1)
		if got != nil {
			t.Fatalf("got %+v want nil", got)
		}
	})

	t.Run("returns nil on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])
		got := fetchSessionFocus(&http.Client{}, "", port)
		if got != nil {
			t.Fatalf("got %+v want nil", got)
		}
	})

	t.Run("returns nil when no focus", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"mode": "git"})
		}))
		defer srv.Close()
		port, _ := strconv.Atoi(srv.URL[len("http://127.0.0.1:"):])
		got := fetchSessionFocus(&http.Client{}, "", port)
		if got != nil {
			t.Fatalf("got %+v want nil", got)
		}
	})
}

func TestFocusKeyArgs_FallsBackToFiles(t *testing.T) {
	sc := &CLIReviewConfig{Files: []string{"a.md", "b.md"}}
	got := FocusKeyArgs(sc)
	if len(got) != 2 || got[0] != "a.md" || got[1] != "b.md" {
		t.Errorf("got %v", got)
	}
}
