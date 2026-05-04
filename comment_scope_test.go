package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// withDaemonFocus replaces the package-level probeDaemonFocusFn for the duration
// of t and restores it on cleanup. nil disables the probe.
func withDaemonFocus(t *testing.T, focus *Focus) {
	t.Helper()
	prev := probeDaemonFocusFn
	probeDaemonFocusFn = func() *Focus { return focus }
	t.Cleanup(func() { probeDaemonFocusFn = prev })
}

func writeReviewFileWithScope(t *testing.T, dir, scope string) {
	t.Helper()
	cj := CritJSON{ActiveDiffScope: scope, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(filepath.Join(dir, ".crit"), cj); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCommentScope(t *testing.T) {
	cases := []struct {
		name        string
		override    commentFocusOverride
		daemonFocus *Focus
		diskScope   string
		wantHead    string
		wantScope   string
		wantErr     string
	}{
		{
			name:      "no override, no daemon, no disk scope -> empty",
			override:  scopeOverrideUnset,
			wantHead:  "",
			wantScope: "",
		},
		{
			name:        "no override, daemon in layer -> inherits both",
			override:    scopeOverrideUnset,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc123", DiffScope: DiffScopeLayer},
			wantHead:    "abc123",
			wantScope:   "layer",
		},
		{
			name:        "no override, daemon in full-stack -> inherits both",
			override:    scopeOverrideUnset,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "def456", DiffScope: DiffScopeFullStack},
			wantHead:    "def456",
			wantScope:   "full_stack",
		},
		{
			name:      "no override, no daemon, disk says layer -> uses disk, no head",
			override:  scopeOverrideUnset,
			diskScope: "layer",
			wantHead:  "",
			wantScope: "layer",
		},
		{
			name:        "override=working-tree, daemon in range -> empty (override wins)",
			override:    scopeOverrideWorkingTree,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc", DiffScope: DiffScopeLayer},
			wantHead:    "",
			wantScope:   "",
		},
		{
			name:     "override=full-stack, no active full-stack -> error",
			override: scopeOverrideFullStack,
			wantErr:  "no active full-stack focus",
		},
		{
			name:        "override=full-stack matches daemon -> uses daemon head",
			override:    scopeOverrideFullStack,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "fs1", DiffScope: DiffScopeFullStack},
			wantHead:    "fs1",
			wantScope:   "full_stack",
		},
		{
			name:        "override=layer, daemon in full-stack -> error (mismatch)",
			override:    scopeOverrideLayer,
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "x", DiffScope: DiffScopeFullStack},
			wantErr:     "no active layer focus",
		},
		{
			name:      "override=layer, no daemon, disk says layer -> uses disk",
			override:  scopeOverrideLayer,
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

			got, err := resolveCommentScope(tc.override, outputDir)
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

func TestRunComment_StampsScopeFromDaemon_LineLevel(t *testing.T) {
	dir := t.TempDir()
	withDaemonFocus(t, &Focus{Kind: FocusRange, HeadSHA: "head1", DiffScope: DiffScopeLayer})

	scope, err := resolveCommentScope(scopeOverrideUnset, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := addCommentToCritJSONScoped("foo.go", 42, 42, "needs review", "tester", "uid", dir, scope); err != nil {
		t.Fatal(err)
	}

	cj, err := loadCritJSON(filepath.Join(dir, ".crit"))
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

	scope, err := resolveCommentScope(scopeOverrideUnset, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := addCommentToCritJSONScoped("foo.go", 1, 1, "x", "tester", "", dir, scope); err != nil {
		t.Fatal(err)
	}

	cj, _ := loadCritJSON(filepath.Join(dir, ".crit"))
	cf := cj.Files["foo.go"]
	if cf.Comments[0].HeadSHA != "" || cf.Comments[0].DiffScope != "" {
		t.Errorf("expected no stamping in working-tree mode, got %+v", cf.Comments[0])
	}
}
