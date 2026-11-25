package session

import "testing"

// changeRequestFoci are daemon foci for every change-request key shape
// focusKeyFor produces. Comments stamped from a scope derived from one of
// them must be visible in that same focus (#1234).
var changeRequestFoci = []struct {
	name    string
	focus   Focus
	wantKey string
}{
	{
		name: "gitlab.com merge request",
		focus: Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, BaseSHA: "b", HeadSHA: "h",
			RemoteBaseProject: "group/sub/project", RemoteHost: "gitlab.com", DiffScope: DiffScopeLayer},
		wantKey: "mr:group/sub/project#7",
	},
	{
		name: "self-managed gitlab merge request",
		focus: Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, BaseSHA: "b", HeadSHA: "h",
			RemoteBaseProject: "group/project", RemoteHost: "gitlab.example.com", DiffScope: DiffScopeLayer},
		wantKey: "mr:gitlab.example.com/group/project#7",
	},
	{
		name: "gitlab merge request without host",
		focus: Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 7, BaseSHA: "b", HeadSHA: "h",
			RemoteBaseProject: "group/project", DiffScope: DiffScopeFullStack},
		wantKey: "mr:group/project#7",
	},
	{
		name: "url-qualified github pull request",
		focus: Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, BaseSHA: "b", HeadSHA: "h",
			RemoteBaseProject: "owner/repo", RemoteHost: "github.com", DiffScope: DiffScopeLayer},
		wantKey: "pr:owner/repo#42",
	},
	{
		name: "url-qualified github enterprise pull request",
		focus: Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, BaseSHA: "b", HeadSHA: "h",
			RemoteBaseProject: "owner/repo", RemoteHost: "ghe.example.com", DiffScope: DiffScopeLayer},
		wantKey: "pr:ghe.example.com/owner/repo#42",
	},
	{
		name: "bare github pull request",
		focus: Focus{Kind: FocusRange, Forge: "github", ChangeNumber: 42, BaseSHA: "b", HeadSHA: "h",
			DiffScope: DiffScopeLayer},
		wantKey: "pr:42",
	},
	{
		name: "bare gitlab merge request",
		focus: Focus{Kind: FocusRange, Forge: "gitlab", ChangeNumber: 17, BaseSHA: "b", HeadSHA: "h",
			DiffScope: DiffScopeLayer},
		wantKey: "mr:17",
	},
	{
		name:    "range without change number",
		focus:   Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h", DiffScope: DiffScopeLayer},
		wantKey: "range:b..h",
	},
}

func TestScopeFromFocusStampsVisibleComments(t *testing.T) {
	for _, tc := range changeRequestFoci {
		t.Run(tc.name, func(t *testing.T) {
			scope := ScopeFromFocus(tc.focus, string(tc.focus.DiffScope))
			c := StampWithFocus(Comment{ID: "c1"}, scope.AsFocus())
			if c.FocusKey != tc.wantKey {
				t.Fatalf("stamped key = %q, want %q", c.FocusKey, tc.wantKey)
			}
			if !visibleInFocus(c, tc.focus) {
				t.Fatalf("comment stamped %+v is hidden in its own focus %+v", c, tc.focus)
			}
		})
	}
}

// TestPullScopeForStampsVisibleComments covers crit pull, the hosted comment
// merge and crit push's discussion refresh: they all stamp through
// PullScopeFor(...).AsFocus(). Pulled comments are always layer-scoped, so the
// check uses the layer view of each focus.
func TestPullScopeForStampsVisibleComments(t *testing.T) {
	for _, tc := range changeRequestFoci {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.focus
			f.DiffScope = DiffScopeLayer
			c := StampWithFocus(Comment{ID: "c1"}, PullScopeFor(&f, nil).AsFocus())
			if !visibleInFocus(c, f) {
				t.Fatalf("pulled comment %+v is hidden in focus %+v", c, f)
			}
		})
	}
}
