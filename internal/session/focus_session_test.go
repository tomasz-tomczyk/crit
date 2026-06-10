package session

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestPersistActiveDiffScope_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &Session{RepoRoot: dir, OutputDir: dir}

	if err := s.persistActiveDiffScope("layer"); err != nil {
		t.Fatal(err)
	}
	cj, err := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if cj.ActiveDiffScope != "layer" {
		t.Errorf("after persist(layer), got %q", cj.ActiveDiffScope)
	}

	// Empty scope must clear, not be skipped.
	if err := s.persistActiveDiffScope(""); err != nil {
		t.Fatal(err)
	}
	cj, _ = readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "" {
		t.Errorf("after persist(\"\"), got %q (should be cleared)", cj.ActiveDiffScope)
	}
}

func TestSetFocus_Range_RebuildsFiles(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "added.txt", "y\n", "add y")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}

	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 1 || s.Files[0].Path != "added.txt" {
		t.Errorf("expected [added.txt], got files=%+v", s.Files)
	}
	if s.Focus.HeadSHA != head {
		t.Errorf("Focus.HeadSHA = %q, want %q", s.Focus.HeadSHA, head)
	}

	// On-disk ActiveDiffScope was persisted.
	cj, _ := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "layer" {
		t.Errorf("disk ActiveDiffScope = %q, want layer", cj.ActiveDiffScope)
	}
}

// TestSetFocus_PostSetSession_PreservesComments is a regression test for B1
// (review). Background: loadCritJSON checks Session.sessionStarted and bails
// out post-SetSession; SetFocus calls it at runtime to repopulate per-file
// Comments after the file list is rebuilt. With the guard active, that
// reload was silently a no-op so any focus change wiped on-disk comments
// from the in-memory session — and the next scheduleWrite would persist
// the empty slate back to disk.
//
// SetFocus must use the Locked variant of loadCritJSON, which skips the
// guard because the caller already holds s.mu. This test pins that
// behavior: after SetSession marks the session started, switching focus
// must keep comments visible on the new file list.
func TestSetFocus_PostSetSession_PreservesComments(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "added.txt", "first\nsecond\n", "add file")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main",
	}

	// First focus: range mode. SetFocus runs the constructor-time path
	// (sessionStarted == 0), so this populates on-disk state cleanly.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}

	// Seed a comment directly on disk so the next SetFocus has something to reload.
	identity := filepath.Join(dir, ".crit")
	cj, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatal(err)
	}
	cf := cj.Files["added.txt"]
	cf.Comments = []Comment{{ID: "c1", Body: "seeded", StartLine: 1, EndLine: 1, Scope: "line"}}
	if cj.Files == nil {
		cj.Files = map[string]CritJSONFile{}
	}
	cj.Files["added.txt"] = cf
	if err := saveCritJSONToDisk(identity, cj); err != nil {
		t.Fatal(err)
	}

	// Simulate Server.SetSession: flip the started flag. Any subsequent
	// loadCritJSON via the public entry point would no-op.
	s.sessionStarted.Store(1)

	// Toggle focus. The internal reload path must use loadCritJSONLocked
	// and pull the seeded comment back into s.Files.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeFullStack, DefaultSHA: base}); err != nil {
		t.Fatal(err)
	}

	var got *FileEntry
	for _, f := range s.Files {
		if f.Path == "added.txt" {
			got = f
			break
		}
	}
	if got == nil {
		t.Fatalf("added.txt missing from rebuilt s.Files: %+v", s.Files)
	}
	if len(got.Comments) != 1 || got.Comments[0].ID != "c1" {
		t.Errorf("comments after focus change = %+v; want one seeded comment", got.Comments)
	}

	// Drain any debounced WriteFiles scheduled by SetFocus before
	// reading on-disk state — otherwise the debounce goroutine and the
	// test reader race on s.Files / RoundSnapshots.
	flushWrites(s)

	// And on-disk state survived (no silent overwrite).
	cj2, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatal(err)
	}
	if cs := cj2.Files["added.txt"].Comments; len(cs) != 1 || cs[0].ID != "c1" {
		t.Errorf("disk comments after focus change = %+v; want one seeded comment", cs)
	}
}

func TestSetFocus_FullStackRequiresDefaultSHA(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
	}
	err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: "b", HeadSHA: "h", DiffScope: DiffScopeFullStack})
	if err == nil {
		t.Fatal("expected error for full-stack without DefaultSHA")
	}
}

func TestSetFocus_WorkingTree_ClearsActiveDiffScope(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "x.txt", "x\n", "x")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main", // working-tree rebuild needs a branch matching DefaultBranch()
	}
	// Start in range/layer.
	if err := s.SetFocus(Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, DiffScope: DiffScopeLayer}); err != nil {
		t.Fatal(err)
	}
	cj, _ := readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "layer" {
		t.Fatalf("setup: ActiveDiffScope=%q want layer", cj.ActiveDiffScope)
	}

	// Toggle to working tree.
	if err := s.SetFocus(Focus{Kind: FocusWorkingTree}); err != nil {
		t.Fatal(err)
	}
	cj, _ = readCritJSONFromDisk(filepath.Join(dir, ".crit"))
	if cj.ActiveDiffScope != "" {
		t.Errorf("on-disk ActiveDiffScope=%q want empty", cj.ActiveDiffScope)
	}
}

// TestSetFocus_RangeToWorkingTree_StashesLastRangeFocus verifies that
// transitioning OUT of a range focus stashes the prior range Focus on the
// session so the UI can render a "Resume PR" affordance.
func TestSetFocus_RangeToWorkingTree_StashesLastRangeFocus(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")
	commitAt(t, dir, "x.txt", "x\n", "x")
	head := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		RepoRoot:  dir,
		OutputDir: dir,
		VCS:       &vcs.GitVCS{},
		Branch:    "main",
	}
	rangeFocus := Focus{Kind: FocusRange, BaseSHA: base, HeadSHA: head, PRNumber: 42, DiffScope: DiffScopeLayer}
	if err := s.SetFocus(rangeFocus); err != nil {
		t.Fatal(err)
	}
	if s.LastRangeFocus != nil {
		t.Errorf("LastRangeFocus should be nil after first range focus; got %+v", s.LastRangeFocus)
	}
	if err := s.SetFocus(Focus{Kind: FocusWorkingTree}); err != nil {
		t.Fatal(err)
	}
	if s.LastRangeFocus == nil {
		t.Fatal("LastRangeFocus should be set after range -> working_tree")
	}
	if s.LastRangeFocus.PRNumber != 42 || s.LastRangeFocus.HeadSHA != head {
		t.Errorf("LastRangeFocus = %+v; want PR=42 head=%s", s.LastRangeFocus, head)
	}
}
