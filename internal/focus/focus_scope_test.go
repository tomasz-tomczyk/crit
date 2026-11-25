package focus

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

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
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	branch := ""
	if vc := vcs.DetectVCS(""); vc != nil {
		branch = vc.CurrentBranch()
	}
	key := daemon.SessionKey(cwd, branch, nil)
	cj := CritJSON{ActiveDiffScope: scope}
	critPath := filepath.Join(dir, "reviews", key)
	if err := review.EnsureReviewFolder(critPath); err != nil {
		t.Fatal(err)
	}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
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
		{
			name:      "override=working-tree -> empty scope",
			override:  ScopeOverrideWorkingTree,
			wantHead:  "",
			wantScope: "",
		},
		{
			name:        "override=layer matches daemon -> uses daemon head",
			override:    ScopeOverrideLayer,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "layer1", DiffScope: DiffScopeLayer},
			wantHead:    "layer1",
			wantScope:   "layer",
		},
		{
			name:      "override=layer matches disk scope -> inherits from disk",
			override:  ScopeOverrideLayer,
			diskScope: "layer",
			wantHead:  "",
			wantScope: "layer",
		},
		{
			name:      "override=full-stack matches disk scope -> inherits from disk",
			override:  ScopeOverrideFullStack,
			diskScope: "full_stack",
			wantHead:  "",
			wantScope: "full_stack",
		},
		{
			name:      "no override, disk scope -> inherits from disk with note",
			override:  ScopeOverrideUnset,
			diskScope: "layer",
			wantHead:  "",
			wantScope: "layer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			if tc.diskScope != "" {
				writeReviewFileWithScope(t, outputDir, tc.diskScope)
			}
			withDaemonFocus(t, tc.daemonFocus)

			var got InheritedScope
			var err error
			stderr := captureStderr(t, func() {
				got, err = resolveCommentScopeForTest(tc.override, outputDir)
			})
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
			if tc.name == "no override, disk scope -> inherits from disk with note" {
				if !strings.Contains(stderr, "stamping comment with diff_scope=") {
					t.Errorf("expected stderr note about disk scope, got %q", stderr)
				}
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

// TestResolveCommentScopeKeepsChangeIdentity guards #1234: the scope must
// carry the project and host of the daemon's change, so comments stamped from
// it get the same focus key as the daemon view.
func TestResolveCommentScopeKeepsChangeIdentity(t *testing.T) {
	foci := []struct {
		name  string
		focus Focus
	}{
		{"gitlab.com merge request", Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "group/project", RemoteHost: "gitlab.com"}},
		{"self-managed gitlab merge request", Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "group/project", RemoteHost: "gitlab.example.com"}},
		{"url-qualified github pull request", Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "owner/repo", RemoteHost: "github.com"}},
		{"bare github pull request", Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, HeadSHA: "h", BaseSHA: "b"}},
	}
	overrides := []struct {
		override CommentFocusOverride
		scope    DiffScope
	}{
		{ScopeOverrideUnset, DiffScopeLayer},
		{ScopeOverrideLayer, DiffScopeLayer},
		{ScopeOverrideFullStack, DiffScopeFullStack},
	}
	for _, fc := range foci {
		for _, ov := range overrides {
			t.Run(fc.name+"/"+string(ov.override), func(t *testing.T) {
				daemonFocus := fc.focus
				daemonFocus.DiffScope = ov.scope
				restore := setProbeDaemonFocusForTest(func() *Focus { f := daemonFocus; return &f })
				t.Cleanup(restore)

				scope, err := resolveCommentScopeForTest(ov.override, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				got := session.StampWithFocus(session.Comment{}, scope.AsFocus())
				want := session.StampWithFocus(session.Comment{}, daemonFocus)
				if got.FocusKey != want.FocusKey || got.DiffScope != want.DiffScope {
					t.Fatalf("stamped (key %q, scope %q), daemon shows (key %q, scope %q)",
						got.FocusKey, got.DiffScope, want.FocusKey, want.DiffScope)
				}
			})
		}
	}
}
