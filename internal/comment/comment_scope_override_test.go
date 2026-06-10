package comment

import (
	"strings"
	"testing"
)

func TestCommentScopeOverrideFromFlag(t *testing.T) {
	cases := []struct {
		in      string
		want    CommentFocusOverride
		wantErr bool
	}{
		{"", ScopeOverrideUnset, false},
		{"layer", ScopeOverrideLayer, false},
		{"full-stack", ScopeOverrideFullStack, false},
		{"full_stack", ScopeOverrideFullStack, false},
		{"working-tree", ScopeOverrideWorkingTree, false},
		{"working_tree", ScopeOverrideWorkingTree, false},
		{"Layer", "", true},      // case-sensitive
		{"FULL-STACK", "", true}, // case-sensitive
		{"bogus", "", true},
		{" layer", "", true}, // no trimming
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := CommentScopeOverrideFromFlag(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("CommentScopeOverrideFromFlag(%q) = (%q, nil); want error", c.in, got)
				}
				if !strings.Contains(err.Error(), "expected layer | full-stack | working-tree") {
					t.Errorf("error %q does not mention valid values", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("CommentScopeOverrideFromFlag(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
