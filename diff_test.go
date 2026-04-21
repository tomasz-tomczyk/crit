package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestComputeLineDiff_BasicChanges(t *testing.T) {
	oldContent := "a\nb\nc"
	newContent := "a\nx\nc\nd"
	diff := ComputeLineDiff(oldContent, newContent)

	expected := []DiffEntry{
		{Type: "unchanged", OldLine: 1, NewLine: 1, Text: "a"},
		{Type: "removed", OldLine: 2, NewLine: 0, Text: "b"},
		{Type: "added", OldLine: 0, NewLine: 2, Text: "x"},
		{Type: "unchanged", OldLine: 3, NewLine: 3, Text: "c"},
		{Type: "added", OldLine: 0, NewLine: 4, Text: "d"},
	}

	if len(diff) != len(expected) {
		t.Fatalf("diff len = %d, want %d\ndiff: %+v", len(diff), len(expected), diff)
	}
	for i, e := range expected {
		if diff[i].Type != e.Type || diff[i].Text != e.Text {
			t.Errorf("diff[%d] type/text = %+v, want %+v", i, diff[i], e)
		}
		if diff[i].OldLine != e.OldLine {
			t.Errorf("diff[%d].OldLine = %d, want %d", i, diff[i].OldLine, e.OldLine)
		}
		if diff[i].NewLine != e.NewLine {
			t.Errorf("diff[%d].NewLine = %d, want %d", i, diff[i].NewLine, e.NewLine)
		}
	}
}

func TestComputeLineDiff_EmptyOld(t *testing.T) {
	diff := ComputeLineDiff("", "a\nb")
	if len(diff) != 2 {
		t.Fatalf("diff len = %d, want 2", len(diff))
	}
	if diff[0].Type != "added" || diff[1].Type != "added" {
		t.Errorf("expected all added, got %+v", diff)
	}
}

func TestComputeLineDiff_EmptyNew(t *testing.T) {
	diff := ComputeLineDiff("a\nb", "")
	if len(diff) != 2 {
		t.Fatalf("diff len = %d, want 2", len(diff))
	}
	if diff[0].Type != "removed" || diff[1].Type != "removed" {
		t.Errorf("expected all removed, got %+v", diff)
	}
}

func TestComputeLineDiff_Identical(t *testing.T) {
	diff := ComputeLineDiff("a\nb\nc", "a\nb\nc")
	for _, e := range diff {
		if e.Type != "unchanged" {
			t.Errorf("expected all unchanged, got %+v", diff)
			break
		}
	}
}

func TestComputeLineDiff_WhitespaceOnlyChanges(t *testing.T) {
	oldContent := "line1\n  indented\nline3"
	newContent := "line1\n    indented\nline3"
	diff := ComputeLineDiff(oldContent, newContent)

	removedCount, addedCount := 0, 0
	for _, e := range diff {
		switch e.Type {
		case "removed":
			removedCount++
		case "added":
			addedCount++
		}
	}
	if removedCount != 1 || addedCount != 1 {
		t.Errorf("whitespace change: removed=%d added=%d, want 1 and 1", removedCount, addedCount)
	}
}

func TestComputeLineDiff_BothEmpty(t *testing.T) {
	diff := ComputeLineDiff("", "")
	if len(diff) != 0 {
		t.Errorf("expected empty diff, got %+v", diff)
	}
}

func TestComputeLineDiff_CompleteReplacement(t *testing.T) {
	diff := ComputeLineDiff("a\nb\nc", "x\ny\nz")
	// Should have 6 entries: 3 removed + 3 added
	removedCount := 0
	addedCount := 0
	for _, e := range diff {
		switch e.Type {
		case "removed":
			removedCount++
		case "added":
			addedCount++
		}
	}
	if removedCount != 3 {
		t.Errorf("removedCount = %d, want 3", removedCount)
	}
	if addedCount != 3 {
		t.Errorf("addedCount = %d, want 3", addedCount)
	}
}

func TestMapOldLineToNew_UnchangedLines(t *testing.T) {
	entries := ComputeLineDiff("a\nb\nc", "a\nb\nc")
	m := MapOldLineToNew(entries)
	for i := 1; i <= 3; i++ {
		if m[i] != i {
			t.Errorf("m[%d] = %d, want %d", i, m[i], i)
		}
	}
}

func TestMapOldLineToNew_WithInsertions(t *testing.T) {
	// Old: a, b, c  →  New: a, x, b, c
	entries := ComputeLineDiff("a\nb\nc", "a\nx\nb\nc")
	m := MapOldLineToNew(entries)
	if m[1] != 1 {
		t.Errorf("m[1] = %d, want 1", m[1])
	}
	if m[2] != 3 {
		t.Errorf("m[2] = %d, want 3", m[2])
	}
	if m[3] != 4 {
		t.Errorf("m[3] = %d, want 4", m[3])
	}
}

func TestMapOldLineToNew_WithRemovals(t *testing.T) {
	// Old: a, b, c  →  New: a, c
	entries := ComputeLineDiff("a\nb\nc", "a\nc")
	m := MapOldLineToNew(entries)
	if m[1] != 1 {
		t.Errorf("m[1] = %d, want 1", m[1])
	}
	// Line 2 was removed; should map to next surviving new line (2, which is "c")
	if m[2] != 2 {
		t.Errorf("m[2] = %d, want 2 (next new line after removed content)", m[2])
	}
	if m[3] != 2 {
		t.Errorf("m[3] = %d, want 2", m[3])
	}
}

func TestDiffEntriesToHunks_BasicChange(t *testing.T) {
	entries := ComputeLineDiff("a\nb\nc", "a\nx\nc")
	hunks := DiffEntriesToHunks(entries)
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(hunks))
	}
	h := hunks[0]
	// Should have context + del + add + context lines
	var dels, adds, ctx int
	for _, l := range h.Lines {
		switch l.Type {
		case "del":
			dels++
		case "add":
			adds++
		case "context":
			ctx++
		}
	}
	if dels != 1 {
		t.Errorf("dels = %d, want 1", dels)
	}
	if adds != 1 {
		t.Errorf("adds = %d, want 1", adds)
	}
	if ctx != 2 {
		t.Errorf("context = %d, want 2", ctx)
	}
}

func TestDiffEntriesToHunks_NoChanges(t *testing.T) {
	entries := ComputeLineDiff("a\nb\nc", "a\nb\nc")
	hunks := DiffEntriesToHunks(entries)
	if len(hunks) != 0 {
		t.Errorf("hunks = %d, want 0 for identical content", len(hunks))
	}
}

func TestDiffEntriesToHunks_AllNew(t *testing.T) {
	entries := ComputeLineDiff("", "a\nb\nc")
	hunks := DiffEntriesToHunks(entries)
	if len(hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(hunks))
	}
	for _, l := range hunks[0].Lines {
		if l.Type != "add" {
			t.Errorf("expected all add lines, got %s", l.Type)
		}
	}
}

// generateCodeLines builds n lines of code-like content (function signatures,
// assignments, comments) to produce realistic benchmark inputs.
func generateCodeLines(n int, seed string) []string {
	lines := make([]string, n)
	for i := range lines {
		switch i % 5 {
		case 0:
			lines[i] = fmt.Sprintf("func %s_%d() {", seed, i)
		case 1:
			lines[i] = fmt.Sprintf("\tx := %d // initialize", i)
		case 2:
			lines[i] = fmt.Sprintf("\tfmt.Println(%q, x)", seed)
		case 3:
			lines[i] = fmt.Sprintf("\treturn // %s line %d", seed, i)
		default:
			lines[i] = "}"
		}
	}
	return lines
}

// mutateLines returns a copy of lines with ~20% changed: a mix of
// modifications, insertions, and deletions spread evenly across the input.
func mutateLines(lines []string) []string {
	out := make([]string, 0, len(lines)+len(lines)/10)
	for i, line := range lines {
		switch {
		case i%15 == 3: // ~7% deleted
			continue
		case i%15 == 7: // ~7% modified
			out = append(out, line+" // modified")
		case i%15 == 11: // ~7% inserted (original kept + new line)
			out = append(out, line)
			out = append(out, fmt.Sprintf("\t// inserted after line %d", i))
		default:
			out = append(out, line)
		}
	}
	return out
}

func BenchmarkComputeLineDiff(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{"100_lines", 100},
		{"1000_lines", 1000},
		{"5000_lines", 5000},
	}

	for _, sz := range sizes {
		oldLines := generateCodeLines(sz.n, "old")
		newLines := mutateLines(oldLines)
		oldContent := strings.Join(oldLines, "\n")
		newContent := strings.Join(newLines, "\n")

		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ComputeLineDiff(oldContent, newContent)
			}
		})
	}
}

func TestDiffEntriesToHunks_SeparateHunks(t *testing.T) {
	// Changes far apart should produce separate hunks (gap > 2*context lines)
	var oldLines, newLines []string
	for i := 1; i <= 30; i++ {
		line := fmt.Sprintf("line%d", i)
		oldLines = append(oldLines, line)
		switch i {
		case 5:
			newLines = append(newLines, "changed5")
		case 25:
			newLines = append(newLines, "changed25")
		default:
			newLines = append(newLines, line)
		}
	}
	old := strings.Join(oldLines, "\n")
	new := strings.Join(newLines, "\n")
	entries := ComputeLineDiff(old, new)
	hunks := DiffEntriesToHunks(entries)
	if len(hunks) != 2 {
		t.Errorf("expected 2 separate hunks for distant changes, got %d", len(hunks))
	}
}
