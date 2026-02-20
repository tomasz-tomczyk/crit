package main

import (
	"strings"
	"testing"
)

func TestGenerateReviewMD_NoComments(t *testing.T) {
	content := "# Title\n\nSome text"
	result := GenerateReviewMD(content, nil)
	if result != content {
		t.Errorf("expected original content, got %q", result)
	}
}

func TestGenerateReviewMD_SingleComment(t *testing.T) {
	content := "line one\nline two\nline three"
	comments := []Comment{
		{ID: "c1", StartLine: 2, EndLine: 2, Body: "Fix this"},
	}
	result := GenerateReviewMD(content, comments)

	if !strings.Contains(result, "line two") {
		t.Error("missing original content")
	}
	if !strings.Contains(result, "> **[REVIEW COMMENT — Line 2]**: Fix this") {
		t.Errorf("missing comment block, got:\n%s", result)
	}
	// Comment should appear after line 2, before line 3
	idx2 := strings.Index(result, "line two")
	idxComment := strings.Index(result, "REVIEW COMMENT")
	idx3 := strings.Index(result, "line three")
	if idx2 > idxComment || idxComment > idx3 {
		t.Error("comment not in correct position")
	}
}

func TestGenerateReviewMD_MultiLineRange(t *testing.T) {
	content := "a\nb\nc\nd"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 3, Body: "Range comment"},
	}
	result := GenerateReviewMD(content, comments)

	if !strings.Contains(result, "Lines 1-3") {
		t.Errorf("expected multi-line header, got:\n%s", result)
	}
	// Comment inserted after end_line (3), so before "d"
	idxComment := strings.Index(result, "REVIEW COMMENT")
	idxD := strings.Index(result, "\nd")
	if idxComment > idxD {
		t.Error("comment should appear before line 'd'")
	}
}

func TestGenerateReviewMD_MultipleCommentsSameEndLine(t *testing.T) {
	content := "a\nb\nc"
	comments := []Comment{
		{ID: "c1", StartLine: 2, EndLine: 2, Body: "First"},
		{ID: "c2", StartLine: 1, EndLine: 2, Body: "Second"},
	}
	result := GenerateReviewMD(content, comments)

	// Both should appear after line 2; sorted by StartLine so c2 (1-2) before c1 (2-2)
	idxFirst := strings.Index(result, "Second")
	idxSecond := strings.Index(result, "First")
	if idxFirst > idxSecond {
		t.Error("comments with same end_line should be sorted by start_line")
	}
}

func TestGenerateReviewMD_MultilineBody(t *testing.T) {
	content := "a\nb"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "line one\nline two"},
	}
	result := GenerateReviewMD(content, comments)

	if !strings.Contains(result, "> line two") {
		t.Errorf("multiline body should be blockquoted, got:\n%s", result)
	}
}

func TestGenerateReviewMD_NoAgentInstructions(t *testing.T) {
	content := "line one"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"},
	}
	result := GenerateReviewMD(content, comments)

	if strings.Contains(result, "Agent Instructions") {
		t.Error("review MD should not contain agent instructions")
	}
	if strings.Contains(result, "crit go") {
		t.Error("review MD should not contain crit go command")
	}
}

func TestGenerateReviewMD_SkipsResolvedComments(t *testing.T) {
	content := "line one\nline two"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this", Resolved: true},
		{ID: "c2", StartLine: 2, EndLine: 2, Body: "And this"},
	}
	result := GenerateReviewMD(content, comments)

	if strings.Contains(result, "Fix this") {
		t.Error("resolved comment should not appear in review MD")
	}
	if !strings.Contains(result, "And this") {
		t.Error("unresolved comment should appear in review MD")
	}
}

func TestFormatComment_SingleLine(t *testing.T) {
	c := Comment{StartLine: 5, EndLine: 5, Body: "hello"}
	result := formatComment(c)
	expected := `> **[REVIEW COMMENT — Line 5]**: hello`
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestFormatComment_MultiLine(t *testing.T) {
	c := Comment{StartLine: 1, EndLine: 3, Body: "hello"}
	result := formatComment(c)
	if !strings.Contains(result, "Lines 1-3") {
		t.Errorf("expected range header, got %q", result)
	}
}
