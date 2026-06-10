package clicmd

import (
	"fmt"
	"testing"
)

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			if got := Plural(tt.n); got != tt.want {
				t.Errorf("Plural(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}
