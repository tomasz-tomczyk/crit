package main

import (
	"strings"
	"testing"
)

func TestCommentScopeOverrideFromFlag(t *testing.T) {
	cases := []struct {
		in      string
		want    commentFocusOverride
		wantErr bool
	}{
		{"", scopeOverrideUnset, false},
		{"layer", scopeOverrideLayer, false},
		{"full-stack", scopeOverrideFullStack, false},
		{"full_stack", scopeOverrideFullStack, false},
		{"working-tree", scopeOverrideWorkingTree, false},
		{"working_tree", scopeOverrideWorkingTree, false},
		{"Layer", "", true},      // case-sensitive
		{"FULL-STACK", "", true}, // case-sensitive
		{"bogus", "", true},
		{" layer", "", true}, // no trimming
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := commentScopeOverrideFromFlag(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("commentScopeOverrideFromFlag(%q) = (%q, nil); want error", c.in, got)
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
				t.Errorf("commentScopeOverrideFromFlag(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
