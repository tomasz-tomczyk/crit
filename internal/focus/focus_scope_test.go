package focus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
)

func withDaemonFocus(t *testing.T, f *Focus) {
	t.Helper()
	restore := setProbeDaemonFocusForTest(func() *Focus {
		if f == nil {
			return nil
		}
		out := *f
		return &out
	})
	t.Cleanup(restore)
}

func writeReviewFileWithScope(t *testing.T, dir, scope string) {
	t.Helper()
	cj := CritJSON{ActiveDiffScope: scope}
	critPath := filepath.Join(dir, ".crit")
	if err := review.EnsureReviewFolder(critPath); err != nil {
		t.Fatal(err)
	}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
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
		got := fetchSessionFocusHTTP(&http.Client{}, "", port)
		if got == nil || got.Kind != FocusRange {
			t.Fatalf("got %+v want range focus", got)
		}
	})

	t.Run("returns nil on error", func(t *testing.T) {
		got := fetchSessionFocusHTTP(&http.Client{}, "", 1)
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
		got := fetchSessionFocusHTTP(&http.Client{}, "", port)
		if got != nil {
			t.Fatalf("got %+v want nil", got)
		}
	})
}

func TestResolveCommentScope(t *testing.T) {
	cases := []struct {
		name        string
		override    CommentFocusOverride
		daemonFocus *Focus
		diskScope   string
		wantHead    string
		wantScope   string
		wantErr     string
	}{
		{
			name:      "no override, no daemon, no disk scope -> empty",
			override:  ScopeOverrideUnset,
			wantHead:  "",
			wantScope: "",
		},
		{
			name:        "no override, daemon in layer -> inherits both",
			override:    ScopeOverrideUnset,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc123", DiffScope: DiffScopeLayer},
			wantHead:    "abc123",
			wantScope:   "layer",
		},
		{
			name:     "override=full-stack, no active full-stack -> error",
			override: ScopeOverrideFullStack,
			wantErr:  "no active full-stack focus",
		},
		{
			name:        "override=full-stack matches daemon -> uses daemon head",
			override:    ScopeOverrideFullStack,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "fs1", DiffScope: DiffScopeFullStack},
			wantHead:    "fs1",
			wantScope:   "full_stack",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			if tc.diskScope != "" {
				writeReviewFileWithScope(t, outputDir, tc.diskScope)
			}
			withDaemonFocus(t, tc.daemonFocus)

			got, err := resolveCommentScopeForTest(tc.override, outputDir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.HeadSHA != tc.wantHead || got.DiffScope != tc.wantScope {
				t.Errorf("got=%+v want head=%q scope=%q", got, tc.wantHead, tc.wantScope)
			}
		})
	}
}

func TestCommentScopeOverrideFromFlag(t *testing.T) {
	cases := []struct {
		in   string
		want CommentFocusOverride
	}{
		{"", ScopeOverrideUnset},
		{"layer", ScopeOverrideLayer},
		{"full-stack", ScopeOverrideFullStack},
		{"working-tree", ScopeOverrideWorkingTree},
	}
	for _, c := range cases {
		got, err := CommentScopeOverrideFromFlag(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestResolvePullScope(t *testing.T) {
	withDaemonFocus(t, &Focus{Kind: FocusRange, HeadSHA: "head1"})
	got := ResolvePullScope(nil)
	if got.HeadSHA != "head1" || got.DiffScope != "layer" {
		t.Errorf("got %+v", got)
	}
}
