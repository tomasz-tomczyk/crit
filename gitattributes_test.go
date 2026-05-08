package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGeneratedRules_MissingFile(t *testing.T) {
	dir := t.TempDir()
	rules, err := parseGeneratedRules(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected no rules, got %d", len(rules))
	}
}

func TestParseGeneratedRules_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	contents := `
# this is a comment
   # indented comment

*.pb.go linguist-generated
`
	writeGitattributes(t, dir, contents)
	rules, err := parseGeneratedRules(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].pattern != "*.pb.go" || rules[0].negate {
		t.Fatalf("unexpected rule: %+v", rules[0])
	}
}

func TestParseGeneratedRules_IgnoresOtherAttributes(t *testing.T) {
	dir := t.TempDir()
	writeGitattributes(t, dir, "*.go linguist-language=Go\n*.bin linguist-vendored\n")
	rules, err := parseGeneratedRules(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestIsGenerated(t *testing.T) {
	tests := []struct {
		name     string
		attrs    string
		path     string
		expected bool
	}{
		{"empty rules", "", "foo.go", false},
		{"basename glob", "*.pb.go linguist-generated\n", "api/foo.pb.go", true},
		{"basename glob negative", "*.pb.go linguist-generated\n", "foo.go", false},
		{"directory glob", "**/generated/** linguist-generated\n", "src/generated/file.go", true},
		{"directory glob no match", "**/generated/** linguist-generated\n", "src/handwritten/file.go", false},
		{"exact path", "vendor/foo.go linguist-generated\n", "vendor/foo.go", true},
		{"trailing slash", "dist/ linguist-generated\n", "dist/bundle.js", true},
		{"trailing slash no match", "dist/ linguist-generated\n", "src/bundle.js", false},
		{"negation overrides", "*.pb.go linguist-generated\napi/keep.pb.go -linguist-generated\n", "api/keep.pb.go", false},
		{"order matters earlier wins not", "api/keep.pb.go -linguist-generated\n*.pb.go linguist-generated\n", "api/keep.pb.go", true},
		{"linguist-generated=true form", "foo.go linguist-generated=true\n", "foo.go", true},
		{"linguist-generated=false form", "*.pb.go linguist-generated\nfoo.pb.go linguist-generated=false\n", "foo.pb.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.attrs != "" {
				writeGitattributes(t, dir, tt.attrs)
			}
			rules, err := parseGeneratedRules(dir)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := isGenerated(tt.path, rules); got != tt.expected {
				t.Fatalf("isGenerated(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func writeGitattributes(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(contents), 0o644); err != nil {
		t.Fatalf("writing .gitattributes: %v", err)
	}
}
