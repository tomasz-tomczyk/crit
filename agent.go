package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// invokeAgent runs the configured agent command with the given prompt.
// If agentCmd contains {prompt}, the placeholder is replaced with the prompt
// as a single argument. Otherwise, the prompt is piped via stdin.
// Returns the agent's stdout output and any error.
func invokeAgent(agentCmd, prompt, cwd string) (string, error) {
	return invokeAgentWithTimeout(agentCmd, prompt, cwd, 10*time.Minute)
}

// invokeAgentWithTimeout is like invokeAgent but with a configurable timeout.
func invokeAgentWithTimeout(agentCmd, prompt, cwd string, timeout time.Duration) (string, error) {
	parts := strings.Fields(agentCmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty agent command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Replace {prompt} placeholder with the actual prompt as a single argument.
	hasPlaceholder := false
	for i, p := range parts {
		if p == "{prompt}" {
			parts[i] = prompt
			hasPlaceholder = true
		}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	if !hasPlaceholder {
		cmd.Stdin = strings.NewReader(prompt)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("agent command failed: %w\nStderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// agentName extracts a short display name from the agent command string.
// e.g. "claude -p" → "claude", "/usr/bin/claude -p" → "claude"
func agentName(agentCmd string) string {
	parts := strings.Fields(agentCmd)
	if len(parts) == 0 {
		return "agent"
	}
	name := parts[0]
	// Strip path prefix
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
