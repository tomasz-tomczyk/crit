package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// hookEnvelope is the JSON structure Claude Code sends on stdin for PermissionRequest hooks.
type hookEnvelope struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

const planTempFile = ".crit-plan.md"

func runHook() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit hook: error reading stdin: %v\n", err)
		writeHookJSON("allow", "")
		return
	}

	var envelope hookEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "crit hook: error parsing input: %v\n", err)
		writeHookJSON("allow", "")
		return
	}

	plan, _ := envelope.ToolInput["plan"].(string)
	if plan == "" {
		writeHookJSON("allow", "")
		return
	}

	// Write plan to temp file in cwd
	if err := os.WriteFile(planTempFile, []byte(plan), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "crit hook: error writing plan: %v\n", err)
		writeHookJSON("allow", "")
		return
	}

	// Resolve port from config
	configPort := 0
	dir, _ := os.Getwd()
	cfg := LoadConfig(dir)
	if cfg.Port != 0 {
		configPort = cfg.Port
	}

	// Start daemon with plan file
	state, err := startDaemon([]string{planTempFile}, configPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit hook: error starting review: %v\n", err)
		os.Remove(planTempFile)
		writeHookJSON("allow", "")
		return
	}

	fmt.Fprintf(os.Stderr, "crit hook: reviewing plan on port %d\n", state.Port)

	// Block until review finishes
	client := &http.Client{Timeout: 24 * time.Hour}
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/api/review-cycle", state.Port),
		"application/json",
		nil,
	)

	// Always clean up daemon and temp file
	defer func() {
		if proc, err := os.FindProcess(state.PID); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
		// Give daemon a moment to write .crit.json before we read it
		time.Sleep(100 * time.Millisecond)
	}()

	if err != nil {
		fmt.Fprintf(os.Stderr, "crit hook: error connecting to review: %v\n", err)
		os.Remove(planTempFile)
		writeHookJSON("allow", "")
		return
	}
	defer resp.Body.Close()

	// Parse feedback
	var feedback struct {
		Status     string `json:"status"`
		ReviewFile string `json:"review_file"`
		Prompt     string `json:"prompt"`
	}

	if resp.StatusCode != http.StatusOK {
		os.Remove(planTempFile)
		writeHookJSON("allow", "")
		return
	}

	if err := json.NewDecoder(resp.Body).Decode(&feedback); err != nil {
		os.Remove(planTempFile)
		writeHookJSON("allow", "")
		return
	}

	// No comments or all resolved → allow
	if feedback.Prompt == "" || strings.Contains(feedback.Prompt, "All comments are resolved") {
		hookCleanup(feedback.ReviewFile)
		writeHookJSON("allow", "")
		return
	}

	// Read .crit.json to get actual comment text for the deny message
	message := formatPlanFeedback(feedback.ReviewFile)
	if message == "" {
		hookCleanup(feedback.ReviewFile)
		writeHookJSON("allow", "")
		return
	}

	hookCleanup(feedback.ReviewFile)
	writeHookJSON("deny", message)
}

// hookCleanup removes the temp plan file and .crit.json created for this review.
func hookCleanup(critJSONPath string) {
	os.Remove(planTempFile)
	if critJSONPath != "" {
		os.Remove(critJSONPath)
	}
}

// formatPlanFeedback reads .crit.json and formats unresolved comments as human-readable feedback.
func formatPlanFeedback(critJSONPath string) string {
	if critJSONPath == "" {
		return ""
	}

	data, err := os.ReadFile(critJSONPath)
	if err != nil {
		return ""
	}

	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return ""
	}

	var lines []string
	for _, file := range cj.Files {
		for _, comment := range file.Comments {
			if comment.Resolved {
				continue
			}
			loc := fmt.Sprintf("Line %d", comment.StartLine)
			if comment.EndLine > comment.StartLine {
				loc = fmt.Sprintf("Lines %d-%d", comment.StartLine, comment.EndLine)
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", loc, comment.Body))
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return "Plan review feedback:\n" + strings.Join(lines, "\n")
}

func writeHookJSON(behavior, message string) {
	type decision struct {
		Behavior string `json:"behavior"`
		Message  string `json:"message,omitempty"`
	}
	type hookOutput struct {
		Decision decision `json:"decision"`
	}
	type response struct {
		HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
	}

	resp := response{
		HookSpecificOutput: hookOutput{
			Decision: decision{
				Behavior: behavior,
				Message:  message,
			},
		},
	}

	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

// installClaudeCodeHook adds the PermissionRequest hook to .claude/settings.json.
// Returns a hint string for the user, or empty if already installed.
func installClaudeCodeHook(force bool) string {
	settingsPath := filepath.Join(".claude", "settings.json")

	// Read existing settings or start fresh
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Navigate to hooks.PermissionRequest
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	permReqRaw, _ := hooks["PermissionRequest"].([]interface{})

	// Check if already installed
	for _, entry := range permReqRaw {
		if m, ok := entry.(map[string]interface{}); ok {
			if m["matcher"] == "ExitPlanMode" {
				if !force {
					fmt.Printf("  Skipped:   %s (hook already configured)\n", settingsPath)
					return "Plans are automatically reviewed with crit before execution"
				}
				// Force: remove existing entry so we re-add below
				break
			}
		}
	}

	// Remove any existing ExitPlanMode entry (for force reinstall)
	var filtered []interface{}
	for _, entry := range permReqRaw {
		if m, ok := entry.(map[string]interface{}); ok {
			if m["matcher"] == "ExitPlanMode" {
				continue
			}
		}
		filtered = append(filtered, entry)
	}

	// Add hook entry
	hookEntry := map[string]interface{}{
		"matcher": "ExitPlanMode",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "crit hook",
				"timeout": 86400,
			},
		},
	}
	filtered = append(filtered, hookEntry)

	hooks["PermissionRequest"] = filtered
	settings["hooks"] = hooks

	// Write back
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Error:     could not marshal settings: %v\n", err)
		return ""
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  Error:     could not create %s: %v\n", filepath.Dir(settingsPath), err)
		return ""
	}

	if err := os.WriteFile(settingsPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  Error:     could not write %s: %v\n", settingsPath, err)
		return ""
	}

	fmt.Printf("  Installed: %s (plan review hook)\n", settingsPath)
	return "Plans are automatically reviewed with crit before execution"
}

// runStdinReview reads raw markdown from stdin, writes to a temp file,
// and runs a normal review session on it. Used by `crit -`.
func runStdinReview() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}
	if len(data) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no input on stdin")
		os.Exit(1)
	}

	if err := os.WriteFile(planTempFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing temp file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(planTempFile)

	runReview([]string{planTempFile})
}
