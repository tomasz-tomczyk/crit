package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveSlug_FromHeading(t *testing.T) {
	content := []byte("# Auth Flow Design\n\nSome content here")
	slug := resolveSlug(content)
	date := time.Now().Format("2006-01-02")
	want := "auth-flow-design-" + date
	if slug != want {
		t.Errorf("resolveSlug = %q, want %q", slug, want)
	}
}

func TestResolveSlug_NoHeading(t *testing.T) {
	content := []byte("No heading here, just text")
	slug := resolveSlug(content)
	if !strings.HasPrefix(slug, "plan-") {
		t.Errorf("resolveSlug fallback = %q, want prefix 'plan-'", slug)
	}
}

func TestResolveSlug_SameHeadingSameDay(t *testing.T) {
	content := []byte("# My Plan\n\nv1")
	slug1 := resolveSlug(content)
	slug2 := resolveSlug([]byte("# My Plan\n\nv2 with changes"))
	if slug1 != slug2 {
		t.Errorf("same heading should produce same slug on same day: %q vs %q", slug1, slug2)
	}
}

func TestPlanMode_RoundTrip(t *testing.T) {
	storageDir := t.TempDir()

	// 1. Save initial plan
	content1 := []byte("# Auth Plan v1\n\nAdd login endpoint")
	ver1, err := savePlanVersion(storageDir, content1)
	if err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if ver1 != 1 {
		t.Fatalf("ver1 = %d, want 1", ver1)
	}

	// 2. Create session from managed file
	currentPath := filepath.Join(storageDir, "current.md")
	session, err := NewSessionFromFiles([]string{currentPath}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}
	applyPlanOverrides(session, storageDir, "auth-plan")

	// 3. Verify session state
	if session.Mode != "plan" {
		t.Errorf("Mode = %q, want plan", session.Mode)
	}
	if len(session.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(session.Files))
	}
	if session.Files[0].Path != "auth-plan.md" {
		t.Errorf("Path = %q, want auth-plan.md", session.Files[0].Path)
	}
	if session.Files[0].Content != string(content1) {
		t.Errorf("Content mismatch")
	}

	// 4. Simulate round 2: save revised plan, re-read
	content2 := []byte("# Auth Plan v2\n\nAdd login + refresh tokens")
	ver2, err := savePlanVersion(storageDir, content2)
	if err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if ver2 != 2 {
		t.Fatalf("ver2 = %d, want 2", ver2)
	}

	// Snapshot previous content (simulating handleRoundCompleteFiles)
	session.Files[0].PreviousContent = session.Files[0].Content

	// Re-read from disk (simulating rereadFileContents)
	data, _ := os.ReadFile(session.Files[0].AbsPath)
	session.Files[0].Content = string(data)
	session.Files[0].FileHash = fileHash(data)

	// 5. Verify inter-round diff is possible
	if session.Files[0].PreviousContent == session.Files[0].Content {
		t.Error("PreviousContent should differ from Content after update")
	}
	if session.Files[0].Content != string(content2) {
		t.Errorf("Content after update = %q, want %q", session.Files[0].Content, string(content2))
	}
}

func TestPlanStorageDir(t *testing.T) {
	dir, err := planStorageDir("auth-flow")
	if err != nil {
		t.Fatalf("planStorageDir: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".crit", "plans", "auth-flow")
	if dir != want {
		t.Errorf("planStorageDir = %q, want %q", dir, want)
	}
}

func TestPlanStorageDir_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := planStorageDir("auth-flow")
	if err == nil {
		t.Error("expected error when HOME is unset")
	}
}

func TestPlanSessionsFile_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := planSessionsFile()
	if err == nil {
		t.Error("expected error when HOME is unset")
	}
}

func TestSavePlanVersion_FirstVersion(t *testing.T) {
	dir := t.TempDir()
	content := []byte("# My Plan\n\nStep 1: Do the thing")

	ver, err := savePlanVersion(dir, content)
	if err != nil {
		t.Fatalf("savePlanVersion: %v", err)
	}
	if ver != 1 {
		t.Errorf("version = %d, want 1", ver)
	}

	data, err := os.ReadFile(filepath.Join(dir, "v001.md"))
	if err != nil {
		t.Fatalf("reading v001.md: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("v001.md content mismatch")
	}

	data, err = os.ReadFile(filepath.Join(dir, "current.md"))
	if err != nil {
		t.Fatalf("reading current.md: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("current.md content mismatch")
	}
}

func TestSavePlanVersion_MultipleVersions(t *testing.T) {
	dir := t.TempDir()

	ver1, _ := savePlanVersion(dir, []byte("version 1"))
	ver2, _ := savePlanVersion(dir, []byte("version 2"))
	ver3, _ := savePlanVersion(dir, []byte("version 3"))

	if ver1 != 1 || ver2 != 2 || ver3 != 3 {
		t.Errorf("versions = %d, %d, %d, want 1, 2, 3", ver1, ver2, ver3)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "current.md"))
	if string(data) != "version 3" {
		t.Errorf("current.md = %q, want %q", string(data), "version 3")
	}

	versions := []string{"version 1", "version 2", "version 3"}
	for i, want := range versions {
		path := filepath.Join(dir, "v"+fmt.Sprintf("%03d", i+1)+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading v%03d.md: %v", i+1, err)
		}
		if string(data) != want {
			t.Errorf("v%03d.md = %q, want %q", i+1, string(data), want)
		}
	}
}

func TestSavePlanVersion_DuplicateContent(t *testing.T) {
	dir := t.TempDir()

	ver1, _ := savePlanVersion(dir, []byte("same content"))
	ver2, _ := savePlanVersion(dir, []byte("same content"))

	if ver1 != 1 || ver2 != 2 {
		t.Errorf("versions = %d, %d, want 1, 2", ver1, ver2)
	}
}

func TestLatestPlanVersion_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	ver := latestPlanVersion(dir)
	if ver != 0 {
		t.Errorf("latestPlanVersion empty dir = %d, want 0", ver)
	}
}

func TestPlanSessionKey(t *testing.T) {
	key1 := planSessionKey("/home/user/project", "auth-flow")
	key2 := planSessionKey("/home/user/project", "auth-flow")
	key3 := planSessionKey("/home/user/project", "other")
	key4 := planSessionKey("/home/user/other", "auth-flow")

	if key1 != key2 {
		t.Error("same inputs should produce same key")
	}
	if key1 == key3 {
		t.Error("different slugs should produce different keys")
	}
	if key1 == key4 {
		t.Error("different cwds should produce different keys")
	}
	if len(key1) != 12 {
		t.Errorf("key length = %d, want 12", len(key1))
	}
}

func TestBuildPlanDaemonArgs(t *testing.T) {
	args := buildPlanDaemonArgs("/tmp/plans/auth-flow/current.md", "/tmp/plans/auth-flow", "auth-flow", 3000, false, false)

	found := false
	for _, a := range args {
		if a == "/tmp/plans/auth-flow/current.md" {
			found = true
		}
	}
	if !found {
		t.Error("expected managed file path in daemon args")
	}

	foundDir := false
	for i, a := range args {
		if a == "--plan-dir" && i+1 < len(args) && args[i+1] == "/tmp/plans/auth-flow" {
			foundDir = true
		}
	}
	if !foundDir {
		t.Error("expected --plan-dir in daemon args")
	}
}

func TestApplyPlanOverrides(t *testing.T) {
	dir := t.TempDir()
	content := "# My Plan\n\nDo things"
	path := filepath.Join(dir, "current.md")
	os.WriteFile(path, []byte(content), 0644)

	session, err := NewSessionFromFiles([]string{path}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}

	applyPlanOverrides(session, dir, "auth-flow")

	if session.Mode != "plan" {
		t.Errorf("Mode = %q, want %q", session.Mode, "plan")
	}
	if session.PlanDir != dir {
		t.Errorf("PlanDir = %q, want %q", session.PlanDir, dir)
	}
	if len(session.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(session.Files))
	}
	if session.Files[0].Path != "auth-flow.md" {
		t.Errorf("display path = %q, want %q", session.Files[0].Path, "auth-flow.md")
	}
}

func TestLookupPlanSlug_NoFile(t *testing.T) {
	// Point planSessionsFile to a temp location
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, ok := lookupPlanSlug("session-abc")
	if ok {
		t.Error("expected no slug for unknown session")
	}
}

func TestSavePlanSlug_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := savePlanSlug("session-1", "auth-flow-2026-03-30"); err != nil {
		t.Fatalf("savePlanSlug: %v", err)
	}

	slug, ok := lookupPlanSlug("session-1")
	if !ok {
		t.Fatal("expected slug to be found")
	}
	if slug != "auth-flow-2026-03-30" {
		t.Errorf("slug = %q, want %q", slug, "auth-flow-2026-03-30")
	}

	// Different session_id should not find it
	_, ok = lookupPlanSlug("session-2")
	if ok {
		t.Error("expected no slug for different session")
	}
}

func TestSavePlanSlug_StableAcrossHeadingChanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Simulate first ExitPlanMode: derive slug from heading, pin it
	content1 := []byte("# Add User Auth\n\nPlan details")
	slug1 := resolveSlug(content1)
	savePlanSlug("session-x", slug1)

	// Simulate second ExitPlanMode: heading changed, but lookup finds pinned slug
	content2 := []byte("# Implement Authentication System\n\nRevised plan")
	slug2 := resolveSlug(content2)

	// Without pinning, these would be different
	if slug1 == slug2 {
		t.Skip("headings produced same slug — test not meaningful")
	}

	// With pinning, we get the original slug
	pinned, ok := lookupPlanSlug("session-x")
	if !ok {
		t.Fatal("expected pinned slug")
	}
	if pinned != slug1 {
		t.Errorf("pinned slug = %q, want original %q", pinned, slug1)
	}
}

func TestSavePlanSlug_PrunesOldEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a stale entry directly
	path, err := planSessionsFile()
	if err != nil {
		t.Fatalf("planSessionsFile: %v", err)
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	stale := map[string]planSessionMapping{
		"old-session": {
			Slug:      "old-plan",
			CreatedAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		},
	}
	data, _ := json.Marshal(stale)
	os.WriteFile(path, data, 0644)

	// Save a new entry — should prune the old one
	savePlanSlug("new-session", "new-plan")

	_, ok := lookupPlanSlug("old-session")
	if ok {
		t.Error("expected stale entry to be pruned")
	}
	slug, ok := lookupPlanSlug("new-session")
	if !ok || slug != "new-plan" {
		t.Error("expected new entry to survive")
	}
}

func TestSavePlanSlug_CorruptJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write corrupt JSON to the sessions file
	path, err := planSessionsFile()
	if err != nil {
		t.Fatalf("planSessionsFile: %v", err)
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("{corrupt json!!!"), 0644)

	// savePlanSlug should return an error instead of silently overwriting
	err = savePlanSlug("session-1", "my-plan")
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}

	// The corrupt file should not have been overwritten
	data, _ := os.ReadFile(path)
	if string(data) != "{corrupt json!!!" {
		t.Errorf("corrupt file was overwritten: got %q", string(data))
	}
}

func TestSavePlanSlug_ConcurrentWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Run multiple concurrent saves; none should lose data
	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			errs <- savePlanSlug(fmt.Sprintf("session-%d", i), fmt.Sprintf("plan-%d", i))
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent savePlanSlug: %v", err)
		}
	}

	// All entries should be present
	for i := 0; i < n; i++ {
		slug, ok := lookupPlanSlug(fmt.Sprintf("session-%d", i))
		if !ok {
			t.Errorf("session-%d not found after concurrent writes", i)
		} else if slug != fmt.Sprintf("plan-%d", i) {
			t.Errorf("session-%d slug = %q, want %q", i, slug, fmt.Sprintf("plan-%d", i))
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Auth Flow Design", "auth-flow-design"},
		{"  spaces  and  stuff  ", "spaces-and-stuff"},
		{"UPPERCASE", "uppercase"},
		{"special!@#chars", "special-chars"},
		{"multiple---dashes", "multiple-dashes"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
