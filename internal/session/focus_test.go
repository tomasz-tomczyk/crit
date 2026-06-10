package session

import (
	"fmt"
	"testing"
)

func TestFocus_DiffBaseSHA(t *testing.T) {
	cases := []struct {
		name string
		f    Focus
		want string
	}{
		{"working tree", Focus{Kind: FocusWorkingTree, BaseRef: "abc"}, "abc"},
		{"range layer", Focus{Kind: FocusRange, BaseSHA: "base", HeadSHA: "head", DiffScope: DiffScopeLayer}, "base"},
		{"range full-stack with default", Focus{Kind: FocusRange, BaseSHA: "base", DefaultSHA: "main", DiffScope: DiffScopeFullStack}, "main"},
		{"range full-stack without default falls back", Focus{Kind: FocusRange, BaseSHA: "base", DiffScope: DiffScopeFullStack}, "base"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.f.DiffBaseSHA(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestFocus_PickerVisible(t *testing.T) {
	cases := []struct {
		f    Focus
		want bool
	}{
		{Focus{Kind: FocusWorkingTree}, false},
		{Focus{Kind: FocusRange, IsStacked: false}, false},
		{Focus{Kind: FocusRange, IsStacked: true}, true},
	}
	for _, c := range cases {
		if got := c.f.PickerVisible(); got != c.want {
			t.Errorf("focus %+v: got %v want %v", c.f, got, c.want)
		}
	}
}

func TestFocus_FullStackAvailable(t *testing.T) {
	if !(Focus{Kind: FocusRange, DefaultSHA: "abc"}).FullStackAvailable() {
		t.Error("range with DefaultSHA should be available")
	}
	if (Focus{Kind: FocusRange, DefaultSHA: ""}).FullStackAvailable() {
		t.Error("range without DefaultSHA should be unavailable")
	}
	if (Focus{Kind: FocusWorkingTree, DefaultSHA: "abc"}).FullStackAvailable() {
		t.Error("working tree never has full-stack available")
	}
}

func TestFocus_ReadOnly(t *testing.T) {
	// v1 is always writable per spec §B "Read-only correction".
	if (Focus{Kind: FocusRange}).ReadOnly() {
		t.Error("v1 Range focus must be writable")
	}
	if (Focus{Kind: FocusWorkingTree}).ReadOnly() {
		t.Error("working tree must be writable")
	}
}

func TestVisibleInFocus(t *testing.T) {
	prFocus := Focus{Kind: FocusRange, DiffScope: DiffScopeLayer, PRNumber: 9}
	prFocusFS := Focus{Kind: FocusRange, DiffScope: DiffScopeFullStack, PRNumber: 9}
	otherPR := Focus{Kind: FocusRange, DiffScope: DiffScopeLayer, PRNumber: 10}

	layer := Comment{DiffScope: "layer", FocusKey: "pr:9"}
	fs := Comment{DiffScope: "full_stack", FocusKey: "pr:9"}
	legacy := Comment{DiffScope: ""}

	cases := []struct {
		name string
		c    Comment
		f    Focus
		want bool
	}{
		{"wt+legacy", legacy, Focus{Kind: FocusWorkingTree}, true},
		{"wt+layer", layer, Focus{Kind: FocusWorkingTree}, false},
		{"range layer + layer comment", layer, prFocus, true},
		{"range layer + full-stack comment", fs, prFocus, false},
		{"range layer + legacy", legacy, prFocus, false},
		{"range full-stack + full-stack", fs, prFocusFS, true},
		{"range full-stack + layer", layer, prFocusFS, false},
		{"different PR hidden", layer, otherPR, false},
		{"empty kind treated as working tree", legacy, Focus{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := visibleInFocus(c.c, c.f); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestStampWithFocus(t *testing.T) {
	wt := Focus{Kind: FocusWorkingTree, BaseRef: "abc"}
	rng := Focus{Kind: FocusRange, HeadSHA: "deadbeef", DiffScope: DiffScopeLayer, PRNumber: 7}

	got := stampWithFocus(Comment{Body: "x"}, wt)
	if got.HeadSHA != "" || got.DiffScope != "" || got.FocusKey != "" {
		t.Errorf("working tree should not stamp: %+v", got)
	}

	got = stampWithFocus(Comment{Body: "x"}, rng)
	if got.HeadSHA != "deadbeef" || got.DiffScope != "layer" {
		t.Errorf("range should stamp: %+v", got)
	}
	if got.FocusKey != "pr:7" {
		t.Errorf("FocusKey=%q want pr:7", got.FocusKey)
	}
}

func TestFocusKeyFor(t *testing.T) {
	cases := []struct {
		name string
		f    Focus
		want string
	}{
		{"working tree", Focus{Kind: FocusWorkingTree}, ""},
		{"empty kind", Focus{}, ""},
		{"range with PR", Focus{Kind: FocusRange, PRNumber: 42, BaseSHA: "aaaaaaa", HeadSHA: "bbbbbbb"}, "pr:42"},
		{"range without PR", Focus{Kind: FocusRange, BaseSHA: "aaaaaaa1234", HeadSHA: "bbbbbbb1234"}, "range:aaaaaaa1234..bbbbbbb1234"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusKeyFor(c.f); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestFocusKeyFor_WireFormat locks down the exact string format produced for
// each focus kind.
func TestFocusKeyFor_WireFormat(t *testing.T) {
	cases := []struct {
		name string
		f    Focus
		want string
	}{
		{
			name: "working_tree",
			f:    Focus{Kind: FocusWorkingTree},
			want: "",
		},
		{
			name: "pr_number",
			f: Focus{
				Kind:     FocusRange,
				PRNumber: 295,
				BaseSHA:  "abc1234deadbeef",
				HeadSHA:  "def5678cafef00d",
			},
			want: "pr:295",
		},
		{
			name: "range_no_pr",
			f: Focus{
				Kind:    FocusRange,
				BaseSHA: "abc1234deadbeef",
				HeadSHA: "def5678cafef00d",
			},
			want: "range:abc1234deadbeef..def5678cafef00d",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusKeyFor(c.f); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestSession_AddComment_StampsScope(t *testing.T) {
	s := newTestSession(t)
	s.Focus = Focus{Kind: FocusRange, HeadSHA: "abc", DiffScope: DiffScopeLayer}
	c, ok := s.AddComment("plan.md", 1, 1, "RIGHT", "hi", "", "u", "u1")
	if !ok {
		t.Fatal("AddComment returned ok=false")
	}
	if c.HeadSHA != "abc" || c.DiffScope != "layer" {
		t.Errorf("scope not stamped: %+v", c)
	}
}

func TestSession_GetComments_FiltersByScope(t *testing.T) {
	s := newTestSession(t)
	s.Focus = Focus{Kind: FocusRange, DiffScope: DiffScopeLayer, HeadSHA: "h"}
	if _, ok := s.AddComment("plan.md", 1, 1, "RIGHT", "layer", "", "u", "u"); !ok {
		t.Fatal("AddComment(layer) failed")
	}
	s.Focus = Focus{Kind: FocusRange, DiffScope: DiffScopeFullStack, HeadSHA: "h"}
	if _, ok := s.AddComment("plan.md", 2, 2, "RIGHT", "fs", "", "u", "u"); !ok {
		t.Fatal("AddComment(full_stack) failed")
	}
	// In layer scope, only "layer" is visible.
	s.Focus = Focus{Kind: FocusRange, DiffScope: DiffScopeLayer, HeadSHA: "h"}
	got := s.GetComments("plan.md")
	if len(got) != 1 || got[0].Body != "layer" {
		t.Errorf("got %+v", got)
	}
}

func TestSession_GetComments_LegacyHiddenInRange(t *testing.T) {
	s := newTestSession(t)
	// Author in working-tree mode (no scope stamp).
	if _, ok := s.AddComment("plan.md", 1, 1, "RIGHT", "old", "", "u", "u"); !ok {
		t.Fatal("AddComment(legacy) failed")
	}
	// Switch focus.
	s.Focus = Focus{Kind: FocusRange, DiffScope: DiffScopeLayer, HeadSHA: "h"}
	if len(s.GetComments("plan.md")) != 0 {
		t.Error("legacy comment should not appear in range view")
	}
	// Switch back — comment reappears.
	s.Focus = Focus{Kind: FocusWorkingTree}
	if len(s.GetComments("plan.md")) != 1 {
		t.Error("legacy comment should be visible in working tree")
	}
}

func TestCarryForwardComment_PreservesScope(t *testing.T) {
	old := Comment{
		ID:        "c1",
		Body:      "carry me",
		HeadSHA:   "abc1234",
		DiffScope: "layer",
	}
	out := carryForwardComment(old, "c2", "2026-04-28T00:00:00Z")
	if out.HeadSHA != "abc1234" {
		t.Errorf("HeadSHA not preserved: %q", out.HeadSHA)
	}
	if out.DiffScope != "layer" {
		t.Errorf("DiffScope not preserved: %q", out.DiffScope)
	}
}

// BenchmarkVisibleInFocus measures the cost of the linear filter scan that
// every GetComments call runs. visibleInFocus is a pure pointer comparison +
// two string compares, so we expect single-digit ns per call. Locking in this
// assumption makes "should we add an index?" decisions evidence-based.
//
// Run: go test -bench=BenchmarkVisibleInFocus -benchmem
func BenchmarkVisibleInFocus(b *testing.B) {
	for _, n := range []int{10, 100, 500, 1000} {
		comments := makeBenchComments(n)
		f := Focus{Kind: FocusRange, PRNumber: 295, DiffScope: DiffScopeLayer}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				count := 0
				for _, c := range comments {
					if visibleInFocus(c, f) {
						count++
					}
				}
				if count == 0 {
					b.Fatal("expected non-zero matches")
				}
			}
		})
	}
}

// makeBenchComments builds n comments split roughly half-and-half between two
// focus keys plus 10% legacy/working-tree, simulating a realistic mix.
func makeBenchComments(n int) []Comment {
	out := make([]Comment, n)
	for i := 0; i < n; i++ {
		switch i % 10 {
		case 0:
			out[i] = Comment{FocusKey: "", DiffScope: ""} // legacy/working-tree
		case 1, 2, 3, 4:
			out[i] = Comment{FocusKey: "pr:295", DiffScope: "layer"}
		default:
			out[i] = Comment{FocusKey: "pr:42", DiffScope: "full_stack"}
		}
	}
	return out
}
