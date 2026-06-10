package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type codexStopHookEvent struct {
	SessionID            string  `json:"session_id"`
	TurnID               string  `json:"turn_id"`
	TranscriptPath       *string `json:"transcript_path"`
	PermissionMode       string  `json:"permission_mode"`
	StopHookActive       bool    `json:"stop_hook_active"`
	LastAssistantMessage *string `json:"last_assistant_message"`
}

var proposedPlanBlockRE = regexp.MustCompile(`(?s)<proposed_plan>\s*(.*?)(?:\s*</proposed_plan>|$)`)

const (
	codexTranscriptInitialTokenBuffer = 64 * 1024
	codexTranscriptMaxTokenSize       = 64 * 1024 * 1024
)

func extractProposedPlan(message string) (string, bool) {
	matches := proposedPlanBlockRE.FindAllStringSubmatch(message, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if plan := strings.TrimSpace(matches[i][1]); plan != "" {
			return plan, true
		}
	}
	return "", false
}

func extractProposedPlanFromCodexTranscript(path, turnID string) (string, bool) {
	if strings.TrimSpace(turnID) == "" {
		return "", false
	}

	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not read transcript %s: %v\n", path, err)
		return "", false
	}
	defer file.Close()

	var latestAssistantMessage string
	inTargetTurn := false
	scanner := newCodexTranscriptScanner(file)
	for scanner.Scan() {
		var line struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type == "turn_context" {
			var payload struct {
				TurnID string `json:"turn_id"`
			}
			if err := json.Unmarshal(line.Payload, &payload); err == nil {
				inTargetTurn = payload.TurnID == turnID
			}
			continue
		}
		if !inTargetTurn || line.Type != "response_item" {
			continue
		}

		var payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(line.Payload, &payload); err != nil {
			continue
		}
		if payload.Type != "message" || payload.Role != "assistant" {
			continue
		}
		var combined strings.Builder
		for _, item := range payload.Content {
			if item.Type == "output_text" {
				combined.WriteString(item.Text)
			}
		}
		latestAssistantMessage = combined.String()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not scan transcript %s: %v\n", path, err)
	}
	return extractProposedPlan(latestAssistantMessage)
}

func newCodexTranscriptScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	initialTokenBuffer := make([]byte, codexTranscriptInitialTokenBuffer)
	scanner.Buffer(initialTokenBuffer, codexTranscriptMaxTokenSize)
	return scanner
}

func proposedPlanFromCodexEvent(event codexStopHookEvent) (string, bool) {
	if event.LastAssistantMessage != nil {
		if plan, ok := extractProposedPlan(*event.LastAssistantMessage); ok {
			return plan, true
		}
	}
	if event.TranscriptPath != nil && strings.TrimSpace(*event.TranscriptPath) != "" {
		return extractProposedPlanFromCodexTranscript(*event.TranscriptPath, event.TurnID)
	}
	return "", false
}
