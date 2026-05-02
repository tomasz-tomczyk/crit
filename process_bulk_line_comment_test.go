package main

import (
	"strings"
	"testing"
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
			cj := &CritJSON{Files: map[string]CritJSONFile{}}
			err := processBulkLineComment(cj, 0, c.entry, "f.go", "alice", "u1", inheritedScope{})
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
