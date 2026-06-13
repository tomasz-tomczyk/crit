package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestResolveAtPrefixedArgs covers the "@file" form that agent file pickers
// (e.g. Claude Code's "@" autocomplete) insert. See issue #656.
func TestResolveAtPrefixedArgs(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "myfile.md")
	if err := os.WriteFile(real, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nope.md")
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"strips @ when stripped path exists", []string{"@" + real}, []string{real}},
		{"strips @ for a directory target", []string{"@" + subdir}, []string{subdir}},
		{"leaves plain path untouched", []string{real}, []string{real}},
		{"leaves @ untouched when stripped path missing", []string{"@" + missing}, []string{"@" + missing}},
		{"bare @ untouched", []string{"@"}, []string{"@"}},
		{"flags untouched, only path normalized", []string{"--no-open", "@" + real}, []string{"--no-open", real}},
		{"multiple @ paths", []string{"@" + real, "@" + missing}, []string{real, "@" + missing}},
		{"nil", nil, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveAtPrefixedArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveAtPrefixedArgs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolveAtPrefixedArgs_RealAtFile verifies that a file genuinely named
// "@foo" is preserved rather than stripped (the literal-exists check wins).
func TestResolveAtPrefixedArgs_RealAtFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("@literal.md", []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolveAtPrefixedArgs([]string{"@literal.md"})
	want := []string{"@literal.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveAtPrefixedArgs = %q, want %q (real @-named file must be preserved)", got, want)
	}
}
