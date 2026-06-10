package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestFocusKeyArgs_PR(t *testing.T) {
	sc := &CLIReviewConfig{Focus: &Focus{Kind: FocusRange, PRNumber: 295}}
	got := FocusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:295" {
		t.Errorf("got %v want [pr:295]", got)
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
