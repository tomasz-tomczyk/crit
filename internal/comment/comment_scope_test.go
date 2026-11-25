package comment

import (
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func withDaemonFocus(t *testing.T, f *Focus) {
	t.Helper()
	restore := SetProbeDaemonFocusFnForTest(func() *session.Focus {
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
	// Use ResolveReviewPath so the fixture key matches production path
	// resolution (including a live daemon session key for this cwd).
	critPath, err := review.ResolveReviewPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{ActiveDiffScope: scope, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCritJSONForOutputDir(t *testing.T) {
	outputDir := t.TempDir()
	writeReviewFileWithScope(t, outputDir, "layer")

	cj, ok := loadCritJSONForOutputDir(outputDir)
	if !ok || cj.ActiveDiffScope != "layer" {
		t.Fatalf("review = %+v, ok = %v; want layer scope", cj, ok)
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
			name:        "no override, daemon in full-stack -> inherits both",
			override:    ScopeOverrideUnset,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "def456", DiffScope: DiffScopeFullStack},
			wantHead:    "def456",
			wantScope:   "full_stack",
		},
		{
			name:      "no override, no daemon, disk says layer -> uses disk, no head",
			override:  ScopeOverrideUnset,
			diskScope: "layer",
			wantHead:  "",
			wantScope: "layer",
		},
		{
			name:        "override=working-tree, daemon in range -> empty (override wins)",
			override:    ScopeOverrideWorkingTree,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc", DiffScope: DiffScopeLayer},
			wantHead:    "",
			wantScope:   "",
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
			name:        "override=layer, daemon in full-stack -> error (mismatch)",
			override:    ScopeOverrideLayer,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "x", DiffScope: DiffScopeFullStack},
			wantErr:     "no active layer focus",
		},
		{
			name:      "override=layer, no daemon, disk says layer -> uses disk",
			override:  ScopeOverrideLayer,
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

			got, err := ResolveCommentScope(tc.override, outputDir)
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

func TestResolveCommentScopeCopiesChangeIdentity(t *testing.T) {
	dir := t.TempDir()
	withDaemonFocus(t, &Focus{
		Kind: FocusRange, Forge: "gitlab", ChangeNumber: 17,
		HeadSHA: "head", BaseSHA: "base", DiffScope: DiffScopeLayer,
	})

	scope, err := ResolveCommentScope(ScopeOverrideUnset, dir)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Forge != "gitlab" || scope.ChangeNumber != 17 {
		t.Fatalf("change identity = (%q, %d), want (gitlab, 17)", scope.Forge, scope.ChangeNumber)
	}
}

func TestRunComment_StampsScopeFromDaemon_LineLevel(t *testing.T) {
	dir := t.TempDir()
	withDaemonFocus(t, &Focus{Kind: FocusRange, HeadSHA: "head1", DiffScope: DiffScopeLayer})

	scope, err := ResolveCommentScope(ScopeOverrideUnset, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := addCommentToCritJSONScoped("foo.go", 42, 42, "needs review", "tester", "uid", dir, scope); err != nil {
		t.Fatal(err)
	}

	critPath, err := review.ResolveReviewPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	cj, err := loadCritJSON(critPath)
	if err != nil {
		t.Fatal(err)
	}
	cf := cj.Files["foo.go"]
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	c := cf.Comments[0]
	if c.HeadSHA != "head1" || c.DiffScope != "layer" {
		t.Errorf("comment not stamped: %+v", c)
	}
}

func TestRunComment_NoStampWithoutDaemonAndDisk(t *testing.T) {
	dir := t.TempDir()
	withDaemonFocus(t, nil)

	scope, err := ResolveCommentScope(ScopeOverrideUnset, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := addCommentToCritJSONScoped("foo.go", 1, 1, "x", "tester", "", dir, scope); err != nil {
		t.Fatal(err)
	}

	critPath, err := review.ResolveReviewPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	cj, _ := loadCritJSON(critPath)
	cf := cj.Files["foo.go"]
	if cf.Comments[0].HeadSHA != "" || cf.Comments[0].DiffScope != "" {
		t.Errorf("expected no stamping in working-tree mode, got %+v", cf.Comments[0])
	}
}

// TestCommentScopeStampsCommentVisibleInDaemonFocus is the #1234 regression:
// a comment written by `crit comment` against a running daemon must carry the
// daemon's focus key, including the project and host of a URL-qualified or
// GitLab change, or the review page hides it.
func TestCommentScopeStampsCommentVisibleInDaemonFocus(t *testing.T) {
	foci := []struct {
		name    string
		focus   Focus
		wantKey string
	}{
		{"gitlab.com merge request", Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "group/project", RemoteHost: "gitlab.com"}, "mr:group/project#7"},
		{"self-managed gitlab merge request", Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "group/project", RemoteHost: "gitlab.example.com"}, "mr:gitlab.example.com/group/project#7"},
		{"url-qualified github pull request", Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, HeadSHA: "h", BaseSHA: "b",
			RemoteBaseProject: "owner/repo", RemoteHost: "github.com"}, "pr:owner/repo#42"},
		{"bare github pull request", Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, HeadSHA: "h", BaseSHA: "b"}, "pr:42"},
	}
	overrides := []struct {
		override CommentFocusOverride
		scope    session.DiffScope
	}{
		{ScopeOverrideUnset, DiffScopeLayer},
		{ScopeOverrideLayer, DiffScopeLayer},
		{ScopeOverrideFullStack, DiffScopeFullStack},
	}
	for _, fc := range foci {
		for _, ov := range overrides {
			t.Run(fc.name+"/"+string(ov.scope)+"/"+string(ov.override), func(t *testing.T) {
				dir := t.TempDir()
				daemonFocus := fc.focus
				daemonFocus.DiffScope = ov.scope
				critPath, err := review.ResolveReviewPath(dir)
				if err != nil {
					t.Fatal(err)
				}
				scope, err := resolveCommentScopeAtPathWithFocus(ov.override, critPath, &daemonFocus)
				if err != nil {
					t.Fatal(err)
				}
				if err := addCommentToCritJSONScoped("main.go", 42, 42, "test", "tester", "", dir, scope); err != nil {
					t.Fatal(err)
				}
				cj, err := loadCritJSON(critPath)
				if err != nil {
					t.Fatal(err)
				}
				got := cj.Files["main.go"].Comments[0]
				want := session.StampWithFocus(session.Comment{}, daemonFocus)
				if got.FocusKey != fc.wantKey || got.FocusKey != want.FocusKey || got.DiffScope != want.DiffScope {
					t.Fatalf("stamped (key %q, scope %q), daemon shows (key %q, scope %q), want key %q",
						got.FocusKey, got.DiffScope, want.FocusKey, want.DiffScope, fc.wantKey)
				}
			})
		}
	}
}
