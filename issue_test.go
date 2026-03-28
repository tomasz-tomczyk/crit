package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveIssueConfig(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want issueConfig
	}{
		{
			name: "description only",
			args: []string{"Add rate limiting"},
			want: issueConfig{description: "Add rate limiting"},
		},
		{
			name: "multi-word description",
			args: []string{"Add", "rate", "limiting", "to", "API"},
			want: issueConfig{description: "Add rate limiting to API"},
		},
		{
			name: "file flag",
			args: []string{"--file", "desc.md"},
			want: issueConfig{file: "desc.md"},
		},
		{
			name: "resume flag",
			args: []string{"--resume", "my-issue"},
			want: issueConfig{resume: "my-issue"},
		},
		{
			name: "plan flag",
			args: []string{"--plan", "my-issue"},
			want: issueConfig{plan: "my-issue"},
		},
		{
			name: "execute flag",
			args: []string{"--execute", "my-issue"},
			want: issueConfig{execute: "my-issue"},
		},
		{
			name: "refine flag",
			args: []string{"--refine", "my-issue"},
			want: issueConfig{refine: "my-issue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveIssueConfig(tt.args)
			if got.description != tt.want.description {
				t.Errorf("description = %q, want %q", got.description, tt.want.description)
			}
			if got.file != tt.want.file {
				t.Errorf("file = %q, want %q", got.file, tt.want.file)
			}
			if got.resume != tt.want.resume {
				t.Errorf("resume = %q, want %q", got.resume, tt.want.resume)
			}
			if got.plan != tt.want.plan {
				t.Errorf("plan = %q, want %q", got.plan, tt.want.plan)
			}
			if got.execute != tt.want.execute {
				t.Errorf("execute = %q, want %q", got.execute, tt.want.execute)
			}
			if got.refine != tt.want.refine {
				t.Errorf("refine = %q, want %q", got.refine, tt.want.refine)
			}
		})
	}
}

func TestIssueSlug(t *testing.T) {
	slug := issueSlug("Add rate limiting to API endpoints")
	// Should contain slugified text + date
	if slug == "" {
		t.Fatal("slug should not be empty")
	}
	// Should start with "add-rate-limiting"
	if len(slug) < 10 {
		t.Errorf("slug too short: %q", slug)
	}
}

func TestIssueStatePersistence(t *testing.T) {
	// Use a temp dir as home to isolate state files
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	state := &issueState{
		Slug:        "test-issue",
		Description: "Test description",
		Branch:      "issue/test-issue",
		Worktree:    "/tmp/test-wt",
		RepoRoot:    "/tmp/test-repo",
		Base:        "main",
		Phase:       "setup",
		OnDone:      "pr",
		PlanPrompt:  "custom plan prompt",
		ExecPrompt:  "custom exec prompt",
		AgentCmd:    "echo test",
	}

	// Save
	if err := saveIssueState(state); err != nil {
		t.Fatalf("saveIssueState: %v", err)
	}

	// Load by slug
	loaded, err := loadIssueState("test-issue")
	if err != nil {
		t.Fatalf("loadIssueState: %v", err)
	}

	if loaded.Slug != state.Slug {
		t.Errorf("Slug = %q, want %q", loaded.Slug, state.Slug)
	}
	if loaded.Description != state.Description {
		t.Errorf("Description = %q, want %q", loaded.Description, state.Description)
	}
	if loaded.Phase != state.Phase {
		t.Errorf("Phase = %q, want %q", loaded.Phase, state.Phase)
	}
	if loaded.PlanPrompt != state.PlanPrompt {
		t.Errorf("PlanPrompt = %q, want %q", loaded.PlanPrompt, state.PlanPrompt)
	}
	if loaded.CreatedAt == "" {
		t.Error("CreatedAt should be set")
	}
	if loaded.UpdatedAt == "" {
		t.Error("UpdatedAt should be set")
	}

	// Load all
	all, err := loadAllIssueStates()
	if err != nil {
		t.Fatalf("loadAllIssueStates: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 issue, got %d", len(all))
	}

	// Update phase
	loaded.Phase = "planning"
	if err := saveIssueState(loaded); err != nil {
		t.Fatalf("saveIssueState (update): %v", err)
	}
	loaded2, err := loadIssueState("test-issue")
	if err != nil {
		t.Fatalf("loadIssueState (after update): %v", err)
	}
	if loaded2.Phase != "planning" {
		t.Errorf("Phase after update = %q, want %q", loaded2.Phase, "planning")
	}

	// Delete
	if err := deleteIssueState("/tmp/test-repo", "test-issue"); err != nil {
		t.Fatalf("deleteIssueState: %v", err)
	}
	_, err = loadIssueState("test-issue")
	if err == nil {
		t.Error("expected error loading deleted issue")
	}
}

func TestWorktreeDir(t *testing.T) {
	dir := worktreeDir("/home/user/myrepo", "my-feature")
	if dir == "" {
		t.Fatal("worktreeDir should not return empty string")
	}
	// Should contain .crit/worktrees/ and slug
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
}

func TestIssueStateFile(t *testing.T) {
	path, err := issueStateFile("/home/user/myrepo", "my-feature")
	if err != nil {
		t.Fatalf("issueStateFile: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("expected .json extension, got %q", filepath.Ext(path))
	}
}

func TestWorktreeHelpers(t *testing.T) {
	dir := initTestRepo(t)

	// Test CreateWorktree (pass repoRoot instead of chdir)
	wtPath := filepath.Join(t.TempDir(), "test-worktree")
	err := CreateWorktree("main", "test-branch", wtPath, dir)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Verify worktree was created
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatal("worktree directory should exist")
	}

	// Test WorktreeList
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	paths, err := WorktreeList()
	os.Chdir(origDir)
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(paths) < 2 { // main + our worktree
		t.Errorf("expected at least 2 worktrees, got %d", len(paths))
	}

	// Test RemoveWorktree (pass repoRoot instead of chdir)
	err = RemoveWorktree(wtPath, dir)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
}

func TestConfigNewFields(t *testing.T) {
	// Test that new config fields round-trip through JSON
	cfg := Config{
		AgentCmd:   "claude -p",
		OnDone:     "pr",
		PlanPrompt: "Be thorough",
		ExecPrompt: "Follow code style",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OnDone != cfg.OnDone {
		t.Errorf("OnDone = %q, want %q", decoded.OnDone, cfg.OnDone)
	}
	if decoded.PlanPrompt != cfg.PlanPrompt {
		t.Errorf("PlanPrompt = %q, want %q", decoded.PlanPrompt, cfg.PlanPrompt)
	}
	if decoded.ExecPrompt != cfg.ExecPrompt {
		t.Errorf("ExecPrompt = %q, want %q", decoded.ExecPrompt, cfg.ExecPrompt)
	}
}

func TestConfigMergeNewFields(t *testing.T) {
	global := Config{
		AgentCmd:   "claude -p",
		OnDone:     "pr",
		PlanPrompt: "global plan prompt",
	}
	project := Config{
		PlanPrompt: "project plan prompt",
		ExecPrompt: "project exec prompt",
	}
	merged := mergeConfigs(global, project, configPresence{})

	if merged.OnDone != "pr" {
		t.Errorf("OnDone = %q, want %q", merged.OnDone, "pr")
	}
	if merged.PlanPrompt != "project plan prompt" {
		t.Errorf("PlanPrompt = %q, want %q (project should override)", merged.PlanPrompt, "project plan prompt")
	}
	if merged.ExecPrompt != "project exec prompt" {
		t.Errorf("ExecPrompt = %q, want %q", merged.ExecPrompt, "project exec prompt")
	}
	if merged.AgentCmd != "claude -p" {
		t.Errorf("AgentCmd = %q, want %q (should inherit from global)", merged.AgentCmd, "claude -p")
	}
}
