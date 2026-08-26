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
