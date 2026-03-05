package main

import (
	"strings"
	"testing"
)

func TestHighlightLines_Go(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	lines := HighlightLines(content, "main.go")
	if lines == nil {
		t.Fatal("expected non-nil lines for Go file")
	}
	if lines[0] != nil {
		t.Error("lines[0] should be nil (1-indexed)")
	}
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 entries, got %d", len(lines))
	}
	if lines[1] == nil {
		t.Fatal("expected non-nil line 1")
	}
	if !strings.Contains(*lines[1], "<span") {
		t.Errorf("line 1 should contain <span> tags, got: %s", *lines[1])
	}
}

func TestHighlightLines_Elixir(t *testing.T) {
	content := "defmodule Foo do\n  def bar, do: :ok\nend\n"
	lines := HighlightLines(content, "lib/foo.ex")
	if lines == nil {
		t.Fatal("expected non-nil lines for Elixir file")
	}
	if lines[2] == nil {
		t.Fatal("expected highlighted line 2")
	}
	if !strings.Contains(*lines[2], "<span") {
		t.Errorf("line 2 should contain <span> tags, got: %s", *lines[2])
	}
}

func TestHighlightLines_UnknownExtension(t *testing.T) {
	lines := HighlightLines("some random content\n", "file.xyz123")
	if lines != nil {
		t.Error("expected nil for unknown file type")
	}
}

func TestHighlightLines_EmptyContent(t *testing.T) {
	lines := HighlightLines("", "main.go")
	if lines != nil {
		t.Error("expected nil for empty content")
	}
}

func TestHighlightLine_Go(t *testing.T) {
	h := HighlightLine("fmt.Println(\"hello\")", "main.go")
	if !strings.Contains(h, "<span") {
		t.Errorf("expected highlighted HTML, got: %s", h)
	}
}

func TestHighlightLine_UnknownLang(t *testing.T) {
	h := HighlightLine("<script>alert('xss')</script>", "file.xyz123")
	if strings.Contains(h, "<script>") {
		t.Error("expected HTML-escaped output for unknown language")
	}
}

func TestHighlightDiffHunks(t *testing.T) {
	content := "package main\n\nfunc hello() {\n}\n"
	hlLines := HighlightLines(content, "main.go")

	hunks := []DiffHunk{{
		Lines: []DiffLine{
			{Type: "context", Content: "package main", NewNum: 1, OldNum: 1},
			{Type: "add", Content: "func hello() {", NewNum: 3},
			{Type: "del", Content: "func old() {", OldNum: 2},
		},
	}}

	HighlightDiffHunks(hunks, hlLines, "main.go")

	for i, line := range hunks[0].Lines {
		if line.HTML == "" {
			t.Errorf("line %d (%s) has empty HTML", i, line.Type)
		}
		if !strings.Contains(line.HTML, "<span") {
			t.Errorf("line %d (%s) should contain <span> tags, got: %s", i, line.Type, line.HTML)
		}
	}
}
