package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParsePRSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"295", 295, false},
		{"https://github.com/a/b/pull/295", 295, false},
		{"https://github.com/a/b/pull/295/files", 295, false},
		{"https://github.com/a/b/pull/295?diff=split", 295, false},
		{"abc", 0, true},
		{"-5", 0, true},
		{"0", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parsePRSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("err=%v wantErr=%v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestParseRangeSpec(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantHead string
		wantErr  bool
	}{
		{"abc..def", "abc", "def", false},
		{"main..feature-x", "main", "feature-x", false},
		{"abc...def", "", "", true},
		{"abc", "", "", true},
		{"..def", "", "", true},
		{"abc..", "", "", true},
		{"", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			b, h, err := parseRangeSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if b != c.wantBase || h != c.wantHead {
				t.Errorf("got (%q, %q) want (%q, %q)", b, h, c.wantBase, c.wantHead)
			}
		})
	}
}

func TestParseScopeSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    DiffScope
		wantErr bool
	}{
		{"", DiffScopeLayer, false},
		{"layer", DiffScopeLayer, false},
		{"full-stack", DiffScopeFullStack, false},
		{"full_stack", DiffScopeFullStack, false},
		{"bogus", "", true},
		{"working-tree", "", true}, // session start does not accept working-tree
		{"working_tree", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseScopeSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("err=%v wantErr=%v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestParseServerFlags_PRAndRangeMutuallyExclusive(t *testing.T) {
	sf := parseServerFlags([]string{"--pr", "1", "--range", "a..b"})
	if sf.prSpec != "1" || sf.rangeSpec != "a..b" {
		t.Fatalf("expected both flags captured, got %+v", sf)
	}
	// resolveFocus will reject this combination — verified separately.
}

func TestParseServerFlags_RangeAndScope(t *testing.T) {
	sf := parseServerFlags([]string{"--range", "a..b", "--scope", "layer"})
	if sf.rangeSpec != "a..b" || sf.scopeSpec != "layer" {
		t.Errorf("got %+v", sf)
	}
}

func TestResolveFocus_PRAndRangeMutuallyExclusive(t *testing.T) {
	_, err := resolveFocus("1", "a..b", "", false, nil, "")
	if err == nil {
		t.Fatal("expected error from mutually-exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q missing 'mutually exclusive'", err)
	}
}

func TestResolveFocus_RangeWithoutVCS(t *testing.T) {
	// When vcs is nil, presence checks are skipped — useful for the
	// daemon-side path where we trust the user's input.
	f, err := resolveFocus("", "abc..def", "", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.Kind != FocusRange {
		t.Fatalf("got %+v want range focus", f)
	}
	if f.BaseSHA != "abc" || f.HeadSHA != "def" {
		t.Errorf("got base=%q head=%q want abc/def", f.BaseSHA, f.HeadSHA)
	}
	if f.DiffScope != DiffScopeLayer {
		t.Errorf("default scope should be layer, got %q", f.DiffScope)
	}
}

func TestResolveFocus_NilWhenNoFlags(t *testing.T) {
	f, err := resolveFocus("", "", "", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Errorf("expected nil focus, got %+v", f)
	}
}

func TestResolveFocus_InvalidScopeRejected(t *testing.T) {
	_, err := resolveFocus("", "a..b", "bogus", false, nil, "")
	if err == nil {
		t.Fatal("expected error from invalid scope")
	}
}

func TestFocusKeyArgs_PR(t *testing.T) {
	sc := &serverConfig{focus: &Focus{Kind: FocusRange, PRNumber: 295}}
	got := focusKeyArgs(sc)
	if len(got) != 1 || got[0] != "pr:295" {
		t.Errorf("got %v want [pr:295]", got)
	}
}

func TestFocusKeyArgs_Range(t *testing.T) {
	sc := &serverConfig{focus: &Focus{Kind: FocusRange, BaseSHA: "abc", HeadSHA: "def"}}
	got := focusKeyArgs(sc)
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
	sc := &serverConfig{files: []string{"a.md", "b.md"}}
	got := focusKeyArgs(sc)
	if len(got) != 2 || got[0] != "a.md" || got[1] != "b.md" {
		t.Errorf("got %v", got)
	}
}
