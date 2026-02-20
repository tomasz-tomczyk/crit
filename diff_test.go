package main

import (
	"testing"
)

func TestComputeLineDiff_BasicChanges(t *testing.T) {
	oldContent := "a\nb\nc"
	newContent := "a\nx\nc\nd"
	diff := ComputeLineDiff(oldContent, newContent)

	expected := []DiffEntry{
		{Type: "unchanged", OldLine: 1, NewLine: 1, Text: "a"},
		{Type: "removed", OldLine: 2, Text: "b"},
		{Type: "added", NewLine: 2, Text: "x"},
		{Type: "unchanged", OldLine: 3, NewLine: 3, Text: "c"},
		{Type: "added", NewLine: 4, Text: "d"},
	}

	if len(diff) != len(expected) {
		t.Fatalf("diff len = %d, want %d\ndiff: %+v", len(diff), len(expected), diff)
	}
	for i, e := range expected {
		if diff[i].Type != e.Type || diff[i].Text != e.Text {
			t.Errorf("diff[%d] = %+v, want %+v", i, diff[i], e)
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

func TestComputeLineDiff_LineNumbers(t *testing.T) {
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

	for i, e := range expected {
		if diff[i].OldLine != e.OldLine {
			t.Errorf("diff[%d].OldLine = %d, want %d", i, diff[i].OldLine, e.OldLine)
		}
		if diff[i].NewLine != e.NewLine {
			t.Errorf("diff[%d].NewLine = %d, want %d", i, diff[i].NewLine, e.NewLine)
		}
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
