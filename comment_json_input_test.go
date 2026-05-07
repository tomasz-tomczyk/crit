package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
	entries, err := parseCommentJSONEntries(data, "-")
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
	_, err := parseCommentJSONEntries(data, "bulk.json")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	msg := err.Error()
	wants := []string{
		"Error parsing JSON from bulk.json at byte ",
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

func TestJSONSourceLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "stdin"},
		{"-", "stdin"},
		{"bulk.json", "bulk.json"},
		{"path/to/file.json", "path/to/file.json"},
	}
	for _, c := range cases {
		got := jsonSourceLabel(c.in)
		if got != c.want {
			t.Errorf("jsonSourceLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatJSONParseError_NoOffset(t *testing.T) {
	// errors.New produces an error with no offset info — exercises the
	// !hasOffset branch in formatJSONParseError.
	err := formatJSONParseError([]byte(`[]`), "test.json", errors.New("generic error"))
	msg := err.Error()
	if !strings.Contains(msg, "Error parsing JSON from test.json") {
		t.Errorf("missing source label: %s", msg)
	}
	if !strings.Contains(msg, "generic error") {
		t.Errorf("missing wrapped error: %s", msg)
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

func TestJSONErrorOffset(t *testing.T) {
	var dst struct{ N int }
	typeErr := json.Unmarshal([]byte(`{"n":"not-a-number"}`), &dst)
	if typeErr == nil {
		t.Fatal("setup: expected UnmarshalTypeError")
	}
	synErr := json.Unmarshal([]byte(`{`), &dst)
	if synErr == nil {
		t.Fatal("setup: expected SyntaxError")
	}

	tests := []struct {
		name       string
		err        error
		wantOK     bool
		wantPosOff bool
	}{
		{"syntax", synErr, true, true},
		{"unmarshal-type", typeErr, true, true},
		{"generic", errors.New("not a json err"), false, false},
		{"nil", nil, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			off, ok := jsonErrorOffset(tc.err)
			if ok != tc.wantOK {
				t.Errorf("ok=%v, want %v", ok, tc.wantOK)
			}
			if tc.wantPosOff && off <= 0 {
				t.Errorf("expected positive offset, got %d", off)
			}
		})
	}
}

func TestVisibleControl(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a\nb", `a\nb`},
		{"a\rb", `a\rb`},
		{"a\tb", `a\tb`},
		{"a\n\r\tb", `a\n\r\tb`},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := visibleControl([]byte(tc.in)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// runCommentJSONScoped calls os.Exit on error and writes to stdout/stderr, so
// it's exercised end-to-end via the helper-subprocess pattern (matching
// TestRunComment_* in main_test.go).

func TestRunCommentJSON_FromFile(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunCommentJSONFile", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Added 1 comment") {
		t.Errorf("expected success summary, got: %s", out)
	}
}

func TestHelperProcess_RunCommentJSONFile(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "test.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	j := filepath.Join(tmp, "comments.json")
	if err := os.WriteFile(j, []byte(`[{"file":"test.go","line":1,"body":"hello"}]`), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	runComment([]string{"--json", "--file", j, "--output", tmp, "--author", "tester"})
}

func TestRunCommentJSON_ParseErrorExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunCommentJSONBad", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit; output: %s", out)
	}
	if !strings.Contains(string(out), "Error parsing JSON from ") {
		t.Errorf("missing formatted parse error: %s", out)
	}
	if !strings.Contains(string(out), `>>>HERE<<<`) {
		t.Errorf("missing snippet marker: %s", out)
	}
}

func TestHelperProcess_RunCommentJSONBad(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(bad, []byte("[\n  {\"body\": \"line one\nline two\"}\n]"), 0o644); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	runComment([]string{"--json", "--file", bad, "--output", tmp})
}

func TestRunCommentJSON_MissingFileExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_RunCommentJSONMissing", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit; output: %s", out)
	}
	if !strings.Contains(string(out), "Error: reading") {
		t.Errorf("missing reading error in output: %s", out)
	}
}

func TestHelperProcess_RunCommentJSONMissing(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	tmp := t.TempDir()
	runComment([]string{"--json", "--file", filepath.Join(tmp, "does-not-exist.json"), "--output", tmp})
}
