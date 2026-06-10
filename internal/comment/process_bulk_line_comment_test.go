package comment

import (
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestProcessBulkLineComment(t *testing.T) {
	cases := []struct {
		name      string
		entry     BulkCommentEntry
		wantErr   string
		wantStart int
		wantEnd   int
	}{
		{
			name:      "explicit Line and EndLine",
			entry:     BulkCommentEntry{Body: "x", Line: 10, EndLine: 12},
			wantStart: 10, wantEnd: 12,
		},
		{
			name:      "Line only defaults EndLine to Line",
			entry:     BulkCommentEntry{Body: "x", Line: 7},
			wantStart: 7, wantEnd: 7,
		},
		{
			name:      "LineSpec single number",
			entry:     BulkCommentEntry{Body: "x", LineSpec: "42"},
			wantStart: 42, wantEnd: 42,
		},
		{
			name:      "LineSpec range",
			entry:     BulkCommentEntry{Body: "x", LineSpec: "5-9"},
			wantStart: 5, wantEnd: 9,
		},
		{
			name:    "LineSpec invalid",
			entry:   BulkCommentEntry{Body: "x", LineSpec: "not-a-number"},
			wantErr: "invalid line spec",
		},
		{
			name:    "LineSpec range with non-numeric end",
			entry:   BulkCommentEntry{Body: "x", LineSpec: "1-bad"},
			wantErr: "invalid line spec",
		},
		{
			name:    "Line zero rejected",
			entry:   BulkCommentEntry{Body: "x"},
			wantErr: "line must be > 0",
		},
		{
			name:    "Line negative rejected",
			entry:   BulkCommentEntry{Body: "x", Line: -3},
			wantErr: "line must be > 0",
		},
		{
			name:      "Line set, LineSpec ignored",
			entry:     BulkCommentEntry{Body: "x", Line: 1, LineSpec: "9-9"},
			wantStart: 1, wantEnd: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cj := &session.CritJSON{Files: map[string]session.CritJSONFile{}}
			err := processBulkLineComment(cj, 0, c.entry, "f.go", "alice", "u1", session.InheritedScope{})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			cf, ok := cj.Files["f.go"]
			if !ok || len(cf.Comments) != 1 {
				t.Fatalf("expected one comment in f.go, got %+v", cj.Files)
			}
			got := cf.Comments[0]
			if got.StartLine != c.wantStart || got.EndLine != c.wantEnd {
				t.Errorf("lines = (%d,%d), want (%d,%d)", got.StartLine, got.EndLine, c.wantStart, c.wantEnd)
			}
		})
	}
}

// TestProcessBulkFileOrLineEntryNormalizesBackslashes ensures Windows-style
// backslash separators in bulk JSON are normalized to forward slashes on all
// platforms. On Unix, filepath.Clean treats backslash as a literal filename
// character, so without the explicit replacement a key like "subdir\file.go"
// would survive into the review file as a single path component.
func TestProcessBulkFileOrLineEntryNormalizesBackslashes(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantKey string
	}{
		{name: "windows separators", input: `subdir\file.go`, wantKey: "subdir/file.go"},
		{name: "mixed separators", input: `a\b/c.go`, wantKey: "a/b/c.go"},
		{name: "nested windows", input: `a\b\c\d.go`, wantKey: "a/b/c/d.go"},
		{name: "already posix", input: "a/b/c.go", wantKey: "a/b/c.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cj := &session.CritJSON{Files: map[string]session.CritJSONFile{}}
			entry := BulkCommentEntry{File: c.input, Body: "x", Line: 1}
			if err := processBulkFileOrLineEntry(cj, 0, entry, "alice", "u1", session.InheritedScope{}); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if _, ok := cj.Files[c.wantKey]; !ok {
				t.Fatalf("expected key %q in cj.Files, got %v", c.wantKey, keysOf(cj.Files))
			}
		})
	}
}

// TestProcessBulkFileOrLineEntryRejectsBackslashTraversal ensures that
// Windows-style traversal paths ("subdir\..\..\etc\passwd") are rejected
// even when the check runs on Unix, where filepath.Clean treats backslash
// as a literal and would pass the raw input through isAbsoluteOrTraversal.
func TestProcessBulkFileOrLineEntryRejectsBackslashTraversal(t *testing.T) {
	cases := []string{
		`subdir\..\..\etc\passwd`,
		`..\etc\passwd`,
		`a\b\..\..\..\secret`,
	}
	for _, input := range cases {
		cj := &session.CritJSON{Files: map[string]session.CritJSONFile{}}
		entry := BulkCommentEntry{File: input, Body: "x", Line: 1}
		err := processBulkFileOrLineEntry(cj, 0, entry, "alice", "u1", session.InheritedScope{})
		if err == nil {
			t.Errorf("input %q: expected traversal error, got nil", input)
		} else if !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("input %q: unexpected error: %v", input, err)
		}
	}
}

func keysOf(m map[string]session.CritJSONFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
