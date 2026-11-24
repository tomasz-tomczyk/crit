package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
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
	ApplyPlanOverrides(session, storageDir, "auth-plan")

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
	setHome(t, "")
	_, err := planStorageDir("auth-flow")
	if err == nil {
		t.Error("expected error when HOME is unset")
	}
}

func TestPlanSessionsFile_NoHome(t *testing.T) {
	setHome(t, "")
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
	if key1 != key4 {
		t.Error("the same global plan slug should produce the same key from different cwds")
	}
	if len(key1) != 12 {
		t.Errorf("key length = %d, want 12", len(key1))
	}
}

func TestBuildPlanDaemonArgs(t *testing.T) {
	args := BuildPlanDaemonArgs("/tmp/plans/auth-flow/current.md", "/tmp/plans/auth-flow", "auth-flow", PlanDaemonFlags{Port: config.PortRange{First: 47800, Last: 47819}})

	foundPort := false
	for i, a := range args {
		if a == "--port" && i+1 < len(args) && args[i+1] == "47800-47819" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Errorf("expected --port 47800-47819 in daemon args, got %v", args)
	}

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

func TestBuildPlanDaemonArgs_PublicURL(t *testing.T) {
	args := BuildPlanDaemonArgs("/tmp/current.md", "/tmp/plans", "slug", PlanDaemonFlags{
		PublicURL: "https://mymac.ts.net",
	})
	want := []string{"--public-url", "https://mymac.ts.net"}
	for _, w := range want {
		found := false
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in daemon args: %v", w, args)
		}
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

	ApplyPlanOverrides(session, dir, "auth-flow")

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

func TestApplyPlanOverrides_DropsOrphansFromInitialLoad(t *testing.T) {
	plan := &FileEntry{Path: "current.md", Status: "modified"}
	orphan := &FileEntry{Path: "saved-plan.md", Status: "removed", Orphaned: true,
		Comments: []Comment{{ID: "saved-comment"}}}
	s := &Session{Files: []*FileEntry{plan, orphan}}

	ApplyPlanOverrides(s, t.TempDir(), "saved-plan")

	if len(s.Files) != 1 || s.Files[0] != plan || plan.Path != "saved-plan.md" {
		t.Fatalf("plan files after removing initial orphan = %+v", s.Files)
	}
}

func TestLookupPlanSlug_NoFile(t *testing.T) {
	// Point planSessionsFile to a temp location
	home := t.TempDir()
	setHome(t, home)

	_, ok := lookupPlanSlug("session-abc")
	if ok {
		t.Error("expected no slug for unknown session")
	}
}

func TestSavePlanSlug_RoundTrip(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	if _, err := savePlanSlug("session-1", "auth-flow-2026-03-30"); err != nil {
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
	setHome(t, home)

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

// Two sessions whose plans share a first heading and a date must get different
// slugs, or their reviews share one storage directory.
func TestResolveHookSlug_SeparatesSessionsWithTheSameHeading(t *testing.T) {
	setHome(t, t.TempDir())
	content := []byte("# Implementation Plan\n\nSteps")

	first := resolveHookSlug("session-a", content)
	second := resolveHookSlug("session-b", content)
	if second != first+"-2" {
		t.Fatalf("slugs = %q and %q, want the second to be the first with -2", first, second)
	}
	if again := resolveHookSlug("session-b", content); again != second {
		t.Errorf("session-b slug changed from %q to %q", second, again)
	}
	if again := resolveHookSlug("session-a", content); again != first {
		t.Errorf("session-a slug changed from %q to %q", first, again)
	}
}

// When the mapping cannot be saved, the slugs must still differ and stay stable.
func TestResolveHookSlug_SeparatesSessionsWithoutTheMapping(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	// A file where ~/.crit should be makes every save fail.
	if err := os.WriteFile(filepath.Join(home, ".crit"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Implementation Plan\n\nSteps")

	first := resolveHookSlug("session-a", content)
	second := resolveHookSlug("session-b", content)
	if first == second {
		t.Fatalf("both sessions got slug %q", first)
	}
	if again := resolveHookSlug("session-a", content); again != first {
		t.Errorf("session-a slug changed from %q to %q", first, again)
	}
}

func TestSavePlanSlug_PrunesOldEntries(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

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
	setHome(t, home)

	// Write corrupt JSON to the sessions file
	path, err := planSessionsFile()
	if err != nil {
		t.Fatalf("planSessionsFile: %v", err)
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("{corrupt json!!!"), 0644)

	// savePlanSlug replaces the unreadable file with a new mapping.
	got, err := savePlanSlug("session-1", "my-plan")
	if err != nil {
		t.Fatalf("savePlanSlug with corrupt file: %v", err)
	}
	if got != "my-plan" {
		t.Errorf("slug = %q, want %q", got, "my-plan")
	}
	m, err := readPlanSessions(path)
	if err != nil {
		t.Fatalf("mapping still unreadable after save: %v", err)
	}
	if len(m) != 1 || m["session-1"].Slug != "my-plan" {
		t.Errorf("mapping = %+v, want only session-1 -> my-plan", m)
	}
}

// A session pinned before the mapping file broke keeps its slug.
func TestResolveHookSlug_PinnedSessionSurvivesCorruptMapping(t *testing.T) {
	setHome(t, t.TempDir())
	content := []byte("# Implementation Plan\n\nSteps")
	first := resolveHookSlug("session-a", content)

	path, err := planSessionsFile()
	if err != nil {
		t.Fatalf("planSessionsFile: %v", err)
	}
	if err := os.WriteFile(path, []byte("{corrupt json!!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := resolveHookSlug("session-a", content); again != first {
		t.Errorf("session-a slug changed from %q to %q after the mapping broke", first, again)
	}
}

func TestSavePlanSlug_Dedup(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	old := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	tests := []struct {
		name    string
		seed    map[string]planSessionMapping
		session string
		slug    string
		want    string
	}{
		{"free slug", nil, "s", "plan", "plan"},
		{"taken once", map[string]planSessionMapping{"a": {"plan", now}}, "s", "plan", "plan-2"},
		{"taken twice", map[string]planSessionMapping{"a": {"plan", now}, "b": {"plan-2", now}}, "s", "plan", "plan-3"},
		{"gap is reused", map[string]planSessionMapping{"a": {"plan", now}, "b": {"plan-3", now}}, "s", "plan", "plan-2"},
		{"own entry is not a collision", map[string]planSessionMapping{"s": {"plan", now}}, "s", "plan", "plan"},
		{"pruned entry frees its slug", map[string]planSessionMapping{"a": {"plan", old}}, "s", "plan", "plan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHome(t, t.TempDir())
			path, err := planSessionsFile()
			if err != nil {
				t.Fatalf("planSessionsFile: %v", err)
			}
			if tt.seed != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(tt.seed)
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := savePlanSlug(tt.session, tt.slug)
			if err != nil {
				t.Fatalf("savePlanSlug: %v", err)
			}
			if got != tt.want {
				t.Errorf("slug = %q, want %q", got, tt.want)
			}
		})
	}
}

// Sessions saving the same slug at once must each get a different one.
func TestSavePlanSlug_ConcurrentSameSlug(t *testing.T) {
	setHome(t, t.TempDir())
	const n = 10
	slugs := make(chan string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			s, err := savePlanSlug(fmt.Sprintf("session-%d", i), "plan")
			if err != nil {
				t.Error(err)
			}
			slugs <- s
		}(i)
	}
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		seen[<-slugs] = true
	}
	if len(seen) != n {
		t.Errorf("%d sessions got %d distinct slugs: %v", n, len(seen), seen)
	}
}

func TestSavePlanSlug_ConcurrentWrites(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Run multiple concurrent saves; none should lose data
	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := savePlanSlug(fmt.Sprintf("session-%d", i), fmt.Sprintf("plan-%d", i))
			errs <- err
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
