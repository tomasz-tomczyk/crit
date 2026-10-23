package session

import "testing"

func TestResolvePullScopePreservesChangeRequestIdentity(t *testing.T) {
	tests := []struct {
		name    string
		focus   Focus
		want    InheritedScope
		wantKey string
	}{
		{
			name: "github pull request",
			focus: Focus{
				Kind: FocusRange, Forge: "github", ChangeNumber: 42,
				BaseSHA: "base-gh", HeadSHA: "head-gh", DiffScope: DiffScopeLayer,
			},
			want: InheritedScope{
				Forge: "github", ChangeNumber: 42,
				BaseSHA: "base-gh", HeadSHA: "head-gh", DiffScope: "layer",
			},
			wantKey: "pr:42",
		},
		{
			name: "gitlab merge request",
			focus: Focus{
				Kind: FocusRange, Forge: "gitlab", ChangeNumber: 17,
				BaseSHA: "base-gl", HeadSHA: "head-gl", DiffScope: DiffScopeLayer,
			},
			want: InheritedScope{
				Forge: "gitlab", ChangeNumber: 17,
				BaseSHA: "base-gl", HeadSHA: "head-gl", DiffScope: "layer",
			},
			wantKey: "mr:17",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := SetProbeDaemonFocusFnForTest(func() *Focus { return &tt.focus })
			defer restore()

			got := ResolvePullScope(nil)
			if got != tt.want {
				t.Fatalf("scope = %+v, want %+v", got, tt.want)
			}
			if key := focusKeyFor(got.AsFocus()); key != tt.wantKey {
				t.Fatalf("focus key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// TestPullScopeFor covers the daemon focus and on-disk fallback used by
// `crit pull` and the hosted comment merge. Pulled comments always get
// DiffScope=layer.
func TestPullScopeFor(t *testing.T) {
	cases := []struct {
		name        string
		daemonFocus *Focus
		cj          *CritJSON
		wantHead    string
		wantScope   string
	}{
		{name: "no daemon, no review file -> empty"},
		{name: "no daemon, empty review file -> empty", cj: &CritJSON{}},
		{
			name:        "daemon range/layer -> head + layer",
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "abc", DiffScope: DiffScopeLayer},
			wantHead:    "abc", wantScope: "layer",
		},
		{
			name:        "daemon range/full-stack -> still layer",
			daemonFocus: &Focus{Kind: FocusRange, HeadSHA: "def", DiffScope: DiffScopeFullStack},
			wantHead:    "def", wantScope: "layer",
		},
		{
			name:        "daemon working-tree -> empty",
			daemonFocus: &Focus{Kind: FocusWorkingTree},
			cj:          &CritJSON{},
		},
		{
			name:      "no daemon, disk says layer -> layer with empty head",
			cj:        &CritJSON{ActiveDiffScope: "layer"},
			wantScope: "layer",
		},
		{
			name:      "no daemon, disk says full_stack -> still layer",
			cj:        &CritJSON{ActiveDiffScope: "full_stack"},
			wantScope: "layer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PullScopeFor(tc.daemonFocus, tc.cj)
			if got.HeadSHA != tc.wantHead || got.DiffScope != tc.wantScope {
				t.Errorf("got=%+v want head=%q scope=%q", got, tc.wantHead, tc.wantScope)
			}
		})
	}
}
