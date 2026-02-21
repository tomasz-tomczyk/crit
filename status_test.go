package main

import (
	"bytes"
	"strings"
	"testing"
)

func testStatus() (*Status, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Status{w: &buf, color: false}, &buf
}

func TestStatusListening(t *testing.T) {
	s, buf := testStatus()
	s.Listening("http://localhost:3247")
	want := "  Listening on http://localhost:3247\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_WithComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(1, 3, true)
	want := "→ Round 1: 3 comments added\n→ Finish review — prompt copied ✓\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_SingleComment(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(2, 1, true)
	want := "→ Round 2: 1 comment added\n→ Finish review — prompt copied ✓\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_NoComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(1, 0, false)
	want := "→ Finish review\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusWaitingForAgent(t *testing.T) {
	s, buf := testStatus()
	s.WaitingForAgent()
	want := "→ Waiting for agent…\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(8)
	want := "→ File updated (8 edits detected)\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated_Singular(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(1)
	want := "→ File updated (1 edit detected)\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated_Zero(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(0)
	if got := buf.String(); got != "" {
		t.Errorf("expected no output for 0 edits, got %q", got)
	}
}

func TestStatusRoundReady_ResolvedAndOpen(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 2, 1)
	want := "→ Round 2: diff ready — 2 resolved, 1 open\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_AllResolved(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 3, 0)
	want := "→ Round 2: diff ready — 3 resolved\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_NoneResolved(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(3, 0, 2)
	want := "→ Round 3: diff ready — 2 open\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_NoPreviousComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 0, 0)
	want := "→ Round 2: diff ready\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusColor_IncludesAnsiCodes(t *testing.T) {
	var buf bytes.Buffer
	s := &Status{w: &buf, color: true}
	s.Listening("http://localhost:3247")
	out := buf.String()
	if !strings.Contains(out, "\033[2m") {
		t.Error("expected dim ANSI code in colored output")
	}
	if !strings.Contains(out, "\033[0m") {
		t.Error("expected reset ANSI code in colored output")
	}
}

func TestStatusColor_GreenInRoundReady(t *testing.T) {
	var buf bytes.Buffer
	s := &Status{w: &buf, color: true}
	s.RoundReady(2, 2, 1)
	out := buf.String()
	if !strings.Contains(out, "\033[32m") {
		t.Error("expected green ANSI code for resolved count")
	}
}
