package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCommentJSONInputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bulk.json")
	want := `[{"body":"hello"}]`
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readCommentJSONInput(path, strings.NewReader("STDIN-IGNORED"))
	if err != nil {
		t.Fatalf("readCommentJSONInput: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadCommentJSONInputStdinDash(t *testing.T) {
	want := `[{"body":"from stdin"}]`
	got, err := readCommentJSONInput("-", strings.NewReader(want))
	if err != nil {
		t.Fatalf("readCommentJSONInput: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadCommentJSONInputStdinDefault(t *testing.T) {
	want := `[]`
	got, err := readCommentJSONInput("", strings.NewReader(want))
	if err != nil {
		t.Fatalf("readCommentJSONInput: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadCommentJSONInputMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	_, err := readCommentJSONInput(missing, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not include path %q", err, missing)
	}
}

func TestParseCommentJSONEntriesValid(t *testing.T) {
	data := []byte(`[{"file":"main.go","line":42,"body":"fix"}]`)
	entries, err := parseCommentJSONEntries(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 1 || entries[0].File != "main.go" || entries[0].Line != 42 {
		t.Errorf("unexpected entries: %+v", entries)
	}
}

func TestParseCommentJSONEntriesRawNewlineInString(t *testing.T) {
	// A raw LF inside a JSON string is the exact failure mode --file is meant
	// to give a better error for. We assemble the bytes manually so the test
	// source itself stays well-formed.
	data := []byte("[\n  {\"body\": \"line one\nline two\"}\n]")
	_, err := parseCommentJSONEntries(data)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	msg := err.Error()
	wants := []string{
		"Error parsing JSON at byte ",
		"line ",
		"column ",
		">>>HERE<<<",
		`\n`, // raw newline rendered as visible escape
	}
	for _, w := range wants {
		if !strings.Contains(msg, w) {
			t.Errorf("error message missing %q\nfull message:\n%s", w, msg)
		}
	}
}

func TestParseCommentFlagsFile(t *testing.T) {
	got := parseCommentFlags([]string{"--json", "--file", "bulk.json"})
	if !got.json {
		t.Error("json flag not set")
	}
	if got.file != "bulk.json" {
		t.Errorf("file = %q, want bulk.json", got.file)
	}

	got = parseCommentFlags([]string{"--json", "-f", "-"})
	if got.file != "-" {
		t.Errorf("file = %q, want -", got.file)
	}
}
