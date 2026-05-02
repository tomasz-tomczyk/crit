package main

import (
	"reflect"
	"testing"
)

func TestBuildDiffArgs(t *testing.T) {
	cases := []struct {
		name    string
		baseRef string
		path    string
		want    []string
	}{
		{"empty base ref omits -r", "", "main.go", []string{"diff", "main.go"}},
		{"with base ref", "main", "main.go", []string{"diff", "-r", "main", "main.go"}},
		{"sha base ref", "abc123", "src/x.rs", []string{"diff", "-r", "abc123", "src/x.rs"}},
		{"path with spaces", "main", "a b/c.go", []string{"diff", "-r", "main", "a b/c.go"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildDiffArgs(c.baseRef, c.path)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("buildDiffArgs(%q, %q) = %v, want %v", c.baseRef, c.path, got, c.want)
			}
		})
	}
}
