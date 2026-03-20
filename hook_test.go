package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteHookJSON(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	writeHookJSON("allow", "")
	w.Close()
	os.Stdout = old

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	var resp struct {
		HookSpecificOutput struct {
			Decision struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}

	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, output)
	}

	if resp.HookSpecificOutput.Decision.Behavior != "allow" {
		t.Errorf("expected behavior=allow, got %q", resp.HookSpecificOutput.Decision.Behavior)
	}

	// Test deny with message
	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	writeHookJSON("deny", "Fix line 5")
	w2.Close()
	os.Stdout = old

	buf2 := make([]byte, 1024)
	n2, _ := r2.Read(buf2)

	var resp2 struct {
		HookSpecificOutput struct {
			Decision struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}

	if err := json.Unmarshal(buf2[:n2], &resp2); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if resp2.HookSpecificOutput.Decision.Behavior != "deny" {
		t.Errorf("expected behavior=deny, got %q", resp2.HookSpecificOutput.Decision.Behavior)
	}
	if resp2.HookSpecificOutput.Decision.Message != "Fix line 5" {
		t.Errorf("expected message='Fix line 5', got %q", resp2.HookSpecificOutput.Decision.Message)
	}
}

func TestFormatPlanFeedback(t *testing.T) {
	// No file
	if got := formatPlanFeedback(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Nonexistent file
	if got := formatPlanFeedback("/tmp/nonexistent-crit-test.json"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Valid .crit.json with unresolved comments
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			".crit-plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 5, EndLine: 5, Body: "Fix this bug"},
					{ID: "c2", StartLine: 10, EndLine: 15, Body: "Needs error handling"},
					{ID: "c3", StartLine: 20, EndLine: 20, Body: "Resolved one", Resolved: true},
				},
			},
		},
	}
	data, _ := json.Marshal(cj)
	tmpFile := filepath.Join(t.TempDir(), ".crit.json")
	os.WriteFile(tmpFile, data, 0644)

	got := formatPlanFeedback(tmpFile)
	if got == "" {
		t.Fatal("expected feedback, got empty")
	}
	if !contains(got, "Line 5: Fix this bug") {
		t.Errorf("missing comment 1 in: %s", got)
	}
	if !contains(got, "Lines 10-15: Needs error handling") {
		t.Errorf("missing comment 2 in: %s", got)
	}
	if contains(got, "Resolved one") {
		t.Errorf("should not include resolved comment in: %s", got)
	}

	// All resolved → empty
	cj2 := CritJSON{
		Files: map[string]CritJSONFile{
			".crit-plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 5, EndLine: 5, Body: "Done", Resolved: true},
				},
			},
		},
	}
	data2, _ := json.Marshal(cj2)
	tmpFile2 := filepath.Join(t.TempDir(), ".crit.json")
	os.WriteFile(tmpFile2, data2, 0644)

	if got := formatPlanFeedback(tmpFile2); got != "" {
		t.Errorf("expected empty for all-resolved, got %q", got)
	}
}

func TestInstallClaudeCodeHook(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// First install
	hint := installClaudeCodeHook(false)
	if hint == "" {
		t.Error("expected hint on first install")
	}

	// Verify settings.json was created
	data, err := os.ReadFile(filepath.Join(".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid settings.json: %v", err)
	}

	hooks := settings["hooks"].(map[string]interface{})
	permReq := hooks["PermissionRequest"].([]interface{})
	if len(permReq) != 1 {
		t.Fatalf("expected 1 hook entry, got %d", len(permReq))
	}

	entry := permReq[0].(map[string]interface{})
	if entry["matcher"] != "ExitPlanMode" {
		t.Errorf("expected matcher=ExitPlanMode, got %v", entry["matcher"])
	}

	// Second install (idempotent)
	hint2 := installClaudeCodeHook(false)
	if hint2 == "" {
		t.Error("expected hint on second install")
	}

	// Verify still only one entry
	data2, _ := os.ReadFile(filepath.Join(".claude", "settings.json"))
	var settings2 map[string]interface{}
	json.Unmarshal(data2, &settings2)
	hooks2 := settings2["hooks"].(map[string]interface{})
	permReq2 := hooks2["PermissionRequest"].([]interface{})
	if len(permReq2) != 1 {
		t.Errorf("expected 1 hook entry after re-install, got %d", len(permReq2))
	}

	// Preserves existing settings
	settings["other_key"] = "preserved"
	data3, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(".claude", "settings.json"), data3, 0644)

	installClaudeCodeHook(true) // force reinstall

	data4, _ := os.ReadFile(filepath.Join(".claude", "settings.json"))
	var settings4 map[string]interface{}
	json.Unmarshal(data4, &settings4)
	if settings4["other_key"] != "preserved" {
		t.Error("force reinstall should preserve existing settings")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
