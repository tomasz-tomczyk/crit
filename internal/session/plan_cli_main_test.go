package session

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestResolvePlanConfig_NameAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	os.WriteFile(path, []byte("# Test Plan"), 0644)

	pc := resolvePlanConfig([]string{"--name", "auth-flow", path})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != path {
		t.Errorf("filePath = %q, want %q", pc.filePath, path)
	}
}

func TestResolvePlanConfig_NameOnly(t *testing.T) {
	pc := resolvePlanConfig([]string{"--name", "auth-flow"})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != "" {
		t.Errorf("filePath should be empty, got %q", pc.filePath)
	}
	if !pc.stdinExpected {
		t.Error("expected stdinExpected=true when no file arg")
	}
}

func TestResolvePlanSlug_UsesNameWhenProvided(t *testing.T) {
	slug := resolvePlanSlug("my-custom-name", []byte("# Some Heading"))
	if slug != "my-custom-name" {
		t.Errorf("resolvePlanSlug with name = %q, want my-custom-name", slug)
	}
}

func TestResolvePlanSlug_DerivesFromContent(t *testing.T) {
	slug := resolvePlanSlug("", []byte("# Auth Flow\n\nDetails here"))
	if slug == "" {
		t.Error("expected non-empty slug derived from content")
	}
	if !strings.Contains(slug, "auth-flow") {
		t.Errorf("slug = %q, expected to contain 'auth-flow'", slug)
	}
}

func TestRunPlanHook_ApprovalEchoesCompleteToolInput(t *testing.T) {
	testutil.SetHome(t, t.TempDir())

	hookInput := json.RawMessage(`{
		"session_id": "session-737",
		"hook_event_name": "PermissionRequest",
		"tool_name": "ExitPlanMode",
		"tool_input": {
			"plan": "# Auth Flow\n\nImplement the auth flow.",
			"planFilePath": "/tmp/auth-flow.md",
			"futureOption": {
				"enabled": true,
				"largeNumber": 9007199254740993,
				"escaped": "\u003cfuture\u003e"
			}
		}
	}`)
	setPlanHookStdin(t, hookInput)

	previousReviewHook := runClaudePlanReviewHook
	runClaudePlanReviewHook = func(sessionID string, content []byte, emitDecision func(bool, string)) {
		if sessionID != "session-737" {
			t.Errorf("sessionID = %q, want session-737", sessionID)
		}
		if got, want := string(content), "# Auth Flow\n\nImplement the auth flow."; got != want {
			t.Errorf("plan content = %q, want %q", got, want)
		}
		emitDecision(true, "")
	}
	t.Cleanup(func() {
		runClaudePlanReviewHook = previousReviewHook
	})

	output := captureHookDecision(t, func() {
		if err := RunPlanHook(); err != nil {
			t.Fatalf("RunPlanHook() error = %v", err)
		}
	})

	var response struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior           string          `json:"behavior"`
				UpdatedInput       json.RawMessage `json:"updatedInput"`
				UpdatedPermissions json.RawMessage `json:"updatedPermissions"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Errorf("hookEventName = %q, want PermissionRequest", response.HookSpecificOutput.HookEventName)
	}
	if response.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", response.HookSpecificOutput.Decision.Behavior)
	}

	var event struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(hookInput, &event); err != nil {
		t.Fatal(err)
	}
	expectedInput := decodeJSONUseNumber(t, event.ToolInput)
	actualInput := decodeJSONUseNumber(t, response.HookSpecificOutput.Decision.UpdatedInput)
	if !reflect.DeepEqual(actualInput, expectedInput) {
		t.Fatalf(
			"decision.updatedInput = %#v, want complete original tool_input %#v",
			actualInput,
			expectedInput,
		)
	}
	if response.HookSpecificOutput.Decision.UpdatedPermissions != nil {
		t.Errorf(
			"unset plan_approve_mode unexpectedly emitted updatedPermissions: %s",
			response.HookSpecificOutput.Decision.UpdatedPermissions,
		)
	}
}

func TestRunPlanHook_ApprovalSetsConfiguredMode(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if err := os.WriteFile(
		filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"plan_approve_mode":"auto"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	hookInput := json.RawMessage(`{
		"session_id": "session-mode",
		"tool_input": {
			"plan": "# Mode switch",
			"planFilePath": "/tmp/mode-switch.md",
			"futureOption": true
		}
	}`)
	setPlanHookStdin(t, hookInput)

	previousReviewHook := runClaudePlanReviewHook
	runClaudePlanReviewHook = func(_ string, _ []byte, emitDecision func(bool, string)) {
		emitDecision(true, "")
	}
	t.Cleanup(func() {
		runClaudePlanReviewHook = previousReviewHook
	})

	output := captureHookDecision(t, func() {
		if err := RunPlanHook(); err != nil {
			t.Fatalf("RunPlanHook() error = %v", err)
		}
	})

	var response struct {
		HookSpecificOutput struct {
			Decision struct {
				UpdatedInput       json.RawMessage  `json:"updatedInput"`
				UpdatedPermissions []map[string]any `json:"updatedPermissions"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}

	if got, want := response.HookSpecificOutput.Decision.UpdatedPermissions, []map[string]any{{
		"type":        "setMode",
		"mode":        "auto",
		"destination": "session",
	}}; !reflect.DeepEqual(got, want) {
		t.Errorf("updatedPermissions = %#v, want %#v", got, want)
	}

	var event struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(hookInput, &event); err != nil {
		t.Fatal(err)
	}
	if got, want := decodeJSONUseNumber(t, response.HookSpecificOutput.Decision.UpdatedInput),
		decodeJSONUseNumber(t, event.ToolInput); !reflect.DeepEqual(got, want) {
		t.Errorf("updatedInput = %#v, want full original tool_input %#v", got, want)
	}
}

func TestRunPlanHook_ApprovalUsesLatestConfiguredMode(t *testing.T) {
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	configPath := filepath.Join(homeDir, ".crit.config.json")
	if err := os.WriteFile(configPath, []byte(`{"plan_approve_mode":"auto"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	setPlanHookStdin(t, []byte(`{
		"session_id": "session-latest-mode",
		"tool_input": {"plan": "# Long-running review"}
	}`))

	previousReviewHook := runClaudePlanReviewHook
	runClaudePlanReviewHook = func(_ string, _ []byte, emitDecision func(bool, string)) {
		if err := os.WriteFile(configPath, []byte(`{"plan_approve_mode":"acceptEdits"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		emitDecision(true, "")
	}
	t.Cleanup(func() {
		runClaudePlanReviewHook = previousReviewHook
	})

	output := captureHookDecision(t, func() {
		if err := RunPlanHook(); err != nil {
			t.Fatalf("RunPlanHook() error = %v", err)
		}
	})

	var response struct {
		HookSpecificOutput struct {
			Decision struct {
				UpdatedPermissions []map[string]string `json:"updatedPermissions"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if got, want := response.HookSpecificOutput.Decision.UpdatedPermissions, []map[string]string{{
		"type":        "setMode",
		"mode":        "acceptEdits",
		"destination": "session",
	}}; !reflect.DeepEqual(got, want) {
		t.Errorf("updatedPermissions = %#v, want %#v", got, want)
	}
}

func TestEmitHookDecision_InvalidPlanApproveModeWarnsAndPreservesOutput(t *testing.T) {
	toolInput := json.RawMessage(`{"plan":"# Auth Flow","futureOption":true}`)

	previousStderr := os.Stderr
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		os.Stderr = previousStderr
		stderrReader.Close()
	})

	output := captureHookDecision(t, func() {
		emitHookDecision(true, "", toolInput, "unrestricted")
	})
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	var response struct {
		HookSpecificOutput struct {
			Decision struct {
				Behavior           string          `json:"behavior"`
				UpdatedInput       json.RawMessage `json:"updatedInput"`
				UpdatedPermissions json.RawMessage `json:"updatedPermissions"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}
	if response.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", response.HookSpecificOutput.Decision.Behavior)
	}
	if response.HookSpecificOutput.Decision.UpdatedPermissions != nil {
		t.Errorf("invalid mode unexpectedly emitted updatedPermissions: %s", response.HookSpecificOutput.Decision.UpdatedPermissions)
	}
	if got, want := decodeJSONUseNumber(t, response.HookSpecificOutput.Decision.UpdatedInput),
		decodeJSONUseNumber(t, toolInput); !reflect.DeepEqual(got, want) {
		t.Errorf("updatedInput = %#v, want %#v", got, want)
	}
	if !strings.Contains(string(stderr), `ignoring invalid plan_approve_mode "unrestricted"`) {
		t.Errorf("stderr = %q, want invalid-mode warning", stderr)
	}
}

func TestPlanApproveModePermissionUpdate_ValidModes(t *testing.T) {
	for _, mode := range []string{
		"default", "manual", "acceptEdits", "plan", "auto", "dontAsk", "bypassPermissions",
	} {
		t.Run(mode, func(t *testing.T) {
			update, valid := planApproveModePermissionUpdate(mode)
			if !valid {
				t.Fatalf("mode %q rejected", mode)
			}
			if update["type"] != "setMode" || update["mode"] != mode || update["destination"] != "session" {
				t.Errorf("update = %#v", update)
			}
		})
	}
}

func decodeJSONUseNumber(t *testing.T, input []byte) any {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(string(input)))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func setPlanHookStdin(t *testing.T, input []byte) {
	t.Helper()

	previousStdin := os.Stdin
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdinWriter.Write(input); err != nil {
		t.Fatal(err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = stdinReader
	t.Cleanup(func() {
		os.Stdin = previousStdin
		stdinReader.Close()
	})
}

func TestEmitHookDecision_DenyBehaviorUnchanged(t *testing.T) {
	toolInput := json.RawMessage(`{"plan":"# Auth Flow","futureOption":true}`)
	output := captureHookDecision(t, func() {
		emitHookDecision(false, "Address the review comments.", toolInput, "bypassPermissions")
	})

	var response struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior     string          `json:"behavior"`
				Message      string          `json:"message"`
				UpdatedInput json.RawMessage `json:"updatedInput"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode hook response: %v", err)
	}

	if response.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Errorf("hookEventName = %q, want PermissionRequest", response.HookSpecificOutput.HookEventName)
	}
	if response.HookSpecificOutput.Decision.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", response.HookSpecificOutput.Decision.Behavior)
	}
	if response.HookSpecificOutput.Decision.Message != "Address the review comments." {
		t.Errorf("message = %q, want review feedback", response.HookSpecificOutput.Decision.Message)
	}
	if response.HookSpecificOutput.Decision.UpdatedInput != nil {
		t.Errorf("deny response unexpectedly included updatedInput: %s", response.HookSpecificOutput.Decision.UpdatedInput)
	}
}

func captureHookDecision(t *testing.T, emit func()) []byte {
	t.Helper()

	previousStdout := os.Stdout
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutWriter
	t.Cleanup(func() {
		os.Stdout = previousStdout
	})

	emit()

	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
