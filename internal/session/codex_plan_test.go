package session

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractProposedPlan(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "extracts multiline plan",
			in:   "before\n<proposed_plan>\n- change auth\n</proposed_plan>\nafter",
			want: "- change auth",
			ok:   true,
		},
		{
			name: "rejects empty plan",
			in:   "<proposed_plan>\n</proposed_plan>",
		},
		{
			name: "accepts unterminated final plan block",
			in:   "before\n<proposed_plan>\n- change auth",
			want: "- change auth",
			ok:   true,
		},
		{
			name: "uses latest non-empty plan block",
			in: strings.Join([]string{
				"<proposed_plan></proposed_plan>",
				"<proposed_plan>\n- first\n</proposed_plan>",
				"<proposed_plan>\n- latest\n</proposed_plan>",
			}, "\n"),
			want: "- latest",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractProposedPlan(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProposedPlanFromCodexEventFallsBackToTranscript(t *testing.T) {
	transcript := writeCodexTranscript(t,
		codexTurnContextLine(t, "turn-1"),
		codexAssistantMessageLine(t, "<proposed_plan>\n- old plan\n</proposed_plan>"),
		codexTurnContextLine(t, "turn-2"),
		codexAssistantMessageLine(t, "visible text\n<proposed_plan>\n- new plan\n</proposed_plan>"),
	)
	visible := "visible text"
	got, ok := proposedPlanFromCodexEvent(codexStopHookEvent{
		LastAssistantMessage: &visible,
		TranscriptPath:       &transcript,
		TurnID:               "turn-2",
	})
	if !ok {
		t.Fatal("expected proposed plan from transcript")
	}
	if got != "- new plan" {
		t.Fatalf("got %q, want %q", got, "- new plan")
	}
}

func TestProposedPlanFromCodexEventUsesTaggedPlanOutsidePlanMode(t *testing.T) {
	visible := "<proposed_plan>\n- should run\n</proposed_plan>"
	got, ok := proposedPlanFromCodexEvent(codexStopHookEvent{
		PermissionMode:       "default",
		LastAssistantMessage: &visible,
	})
	if !ok {
		t.Fatal("expected tagged plan outside formal plan mode")
	}
	if got != "- should run" {
		t.Fatalf("got %q, want %q", got, "- should run")
	}
}

func TestProposedPlanFromCodexEventDoesNotReuseOldTranscriptPlan(t *testing.T) {
	transcript := writeCodexTranscript(t,
		codexTurnContextLine(t, "turn-1"),
		codexAssistantMessageLine(t, "<proposed_plan>\n- old plan\n</proposed_plan>"),
		codexTurnContextLine(t, "turn-2"),
		codexAssistantMessageLine(t, "ordinary final answer"),
	)
	visible := "ordinary final answer"
	if plan, ok := proposedPlanFromCodexEvent(codexStopHookEvent{
		LastAssistantMessage: &visible,
		TranscriptPath:       &transcript,
		TurnID:               "turn-2",
	}); ok {
		t.Fatalf("did not expect old plan to be reused, got %q", plan)
	}
}

func TestRunCodexPlanHookReviewsTaggedPlanWhenStopHookActive(t *testing.T) {
	prevHook := runCodexPlanReviewHook
	t.Cleanup(func() { runCodexPlanReviewHook = prevHook })

	var gotSessionID string
	var gotContent string
	runCodexPlanReviewHook = func(sessionID string, content []byte) {
		gotSessionID = sessionID
		gotContent = string(content)
	}

	visible := "<proposed_plan>\n- revised plan\n</proposed_plan>"
	runCodexPlanHookWithInput(t, codexStopHookEvent{
		SessionID:            "codex-session",
		StopHookActive:       true,
		LastAssistantMessage: &visible,
	})

	if gotSessionID != "codex-session" {
		t.Fatalf("session id = %q, want codex-session", gotSessionID)
	}
	if gotContent != "- revised plan" {
		t.Fatalf("review content = %q, want revised plan", gotContent)
	}
}

func TestRunCodexPlanHookSkipsStopHookActiveWithoutTaggedPlan(t *testing.T) {
	prevHook := runCodexPlanReviewHook
	t.Cleanup(func() { runCodexPlanReviewHook = prevHook })

	called := false
	runCodexPlanReviewHook = func(string, []byte) {
		called = true
	}

	visible := "ordinary assistant message"
	runCodexPlanHookWithInput(t, codexStopHookEvent{
		StopHookActive:       true,
		LastAssistantMessage: &visible,
	})

	if called {
		t.Fatal("did not expect review hook to run without a tagged proposed plan")
	}
}

func TestProposedPlanFromCodexEventFallsBackToTranscriptWhenStopHookActive(t *testing.T) {
	transcript := writeCodexTranscript(t,
		codexTurnContextLine(t, "turn-1"),
		codexAssistantMessageLine(t, "<proposed_plan>\n- old plan\n</proposed_plan>"),
		codexAssistantMessageLine(t, "<proposed_plan>\n- revised plan\n</proposed_plan>"),
	)
	visible := "ordinary assistant message"
	plan, ok := proposedPlanFromCodexEvent(codexStopHookEvent{
		StopHookActive:       true,
		LastAssistantMessage: &visible,
		TranscriptPath:       &transcript,
		TurnID:               "turn-1",
	})
	if !ok {
		t.Fatal("expected Stop hook recursion to review transcript plan")
	}
	if plan != "- revised plan" {
		t.Fatalf("got %q, want revised plan", plan)
	}
}

func TestProposedPlanFromCodexEventDoesNotReuseSameTurnStaleTranscriptPlan(t *testing.T) {
	transcript := writeCodexTranscript(t,
		codexTurnContextLine(t, "turn-1"),
		codexAssistantMessageLine(t, "<proposed_plan>\n- stale plan\n</proposed_plan>"),
		codexAssistantMessageLine(t, "ordinary assistant message"),
	)
	visible := "ordinary assistant message"
	if plan, ok := proposedPlanFromCodexEvent(codexStopHookEvent{
		StopHookActive:       true,
		LastAssistantMessage: &visible,
		TranscriptPath:       &transcript,
		TurnID:               "turn-1",
	}); ok {
		t.Fatalf("did not expect stale same-turn plan to be reused, got %q", plan)
	}
}

func TestRunCodexPlanHookBlocksOnMalformedInput(t *testing.T) {
	out := runCodexPlanHookWithRawInput(t, "{")
	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &decision); err != nil {
		t.Fatalf("decode decision %q: %v", out, err)
	}
	if decision.Decision != "block" {
		t.Fatalf("decision = %q, want block", decision.Decision)
	}
	if !strings.Contains(decision.Reason, "could not parse") {
		t.Fatalf("reason = %q, want parse failure", decision.Reason)
	}
}

func runCodexPlanHookWithInput(t *testing.T, event codexStopHookEvent) {
	t.Helper()

	prevStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		os.Stdin = prevStdin
		_ = r.Close()
	})
	os.Stdin = r
	if err := json.NewEncoder(w).Encode(event); err != nil {
		t.Fatalf("encode event: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	RunCodexPlanHook()
}

func runCodexPlanHookWithRawInput(t *testing.T, input string) string {
	t.Helper()

	prevStdin := os.Stdin
	prevStdout := os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		os.Stdin = prevStdin
		os.Stdout = prevStdout
		_ = inR.Close()
		_ = outR.Close()
	})
	os.Stdin = inR
	os.Stdout = outW
	if _, err := inW.WriteString(input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := inW.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	RunCodexPlanHook()
	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	data, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return strings.TrimSpace(string(data))
}

type codexTranscriptFixtureLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func writeCodexTranscript(t *testing.T, lines ...codexTranscriptFixtureLine) string {
	t.Helper()

	transcript := filepath.Join(t.TempDir(), "rollout.jsonl")
	var b strings.Builder
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(transcript, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return transcript
}

func codexTurnContextLine(t *testing.T, turnID string) codexTranscriptFixtureLine {
	t.Helper()
	payload, err := json.Marshal(struct {
		TurnID string `json:"turn_id"`
	}{TurnID: turnID})
	if err != nil {
		t.Fatalf("marshal turn context: %v", err)
	}
	return codexTranscriptFixtureLine{Type: "turn_context", Payload: payload}
}

func codexAssistantMessageLine(t *testing.T, text string) codexTranscriptFixtureLine {
	t.Helper()
	payload, err := json.Marshal(struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{
		Type: "message",
		Role: "assistant",
		Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "output_text", Text: text},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant message: %v", err)
	}
	return codexTranscriptFixtureLine{Type: "response_item", Payload: payload}
}
