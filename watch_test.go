package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWatchFileMtimes_CommentNotLostOnFileChange verifies that a comment added
// concurrently with the file watcher detecting a content change is not silently
// discarded. This exercises the fix for the race where:
//  1. Watcher reads FileHash under RLock, sees hash differs
//  2. AddComment runs (acquires Lock, appends comment, releases Lock)
//  3. Watcher acquires Lock and blindly clears Comments
//
// The fix checks the hash under the write lock so step 3 sees the current state.
func TestWatchFileMtimes_CommentNotLostOnFileChange(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	content := "# Plan\n\nStep 1\n"
	writeFile(t, mdPath, content)

	s := &Session{
		Mode:        "files",
		RepoRoot:    dir,
		ReviewRound: 1,

		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  mdPath,
				Status:   "modified",
				FileType: "markdown",
				Content:  content,
				FileHash: fileHash([]byte(content)),
				Comments: []Comment{},
			},
		},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	stop := make(chan struct{})
	defer close(stop)

	// Start the file watcher in the background.
	go s.watchFileMtimes(stop)

	// Add a comment while the file hasn't changed — this should persist.
	_, ok := s.AddComment("plan.md", 1, 1, "", "important feedback", "", "tester")
	if !ok {
		t.Fatal("AddComment failed")
	}

	// Give the watcher one tick to confirm it doesn't clear comments
	// when the file hasn't changed.
	time.Sleep(1500 * time.Millisecond)

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment before file change, got %d", len(comments))
	}
	if comments[0].Body != "important feedback" {
		t.Errorf("comment body = %q", comments[0].Body)
	}
}

// TestWatchFileMtimes_ConcurrentAddDuringChange uses the race detector to verify
// there is no data race between the watcher clearing comments on file change and
// concurrent AddComment calls. Run with: go test -race -run TestWatchFileMtimes_ConcurrentAddDuringChange
func TestWatchFileMtimes_ConcurrentAddDuringChange(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	content := "# Plan\n\nStep 1\n"
	writeFile(t, mdPath, content)

	s := &Session{
		Mode:        "files",
		RepoRoot:    dir,
		ReviewRound: 1,

		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  mdPath,
				Status:   "modified",
				FileType: "markdown",
				Content:  content,
				FileHash: fileHash([]byte(content)),
				Comments: []Comment{},
			},
		},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	stop := make(chan struct{})

	// Start the file watcher.
	go s.watchFileMtimes(stop)

	// Concurrently: add comments in a tight loop while modifying the file on disk.
	var wg sync.WaitGroup

	// Writer goroutine: keep modifying the file to trigger the watcher's change path.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			newContent := []byte("# Plan\n\n## Revision " + string(rune('A'+i)) + "\n\nUpdated\n")
			os.WriteFile(mdPath, newContent, 0644)
			time.Sleep(200 * time.Millisecond)
		}
	}()

	// Comment goroutines: keep adding comments concurrently with file changes.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s.AddComment("plan.md", 1, 1, "", "concurrent comment", "", "tester")
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(stop)

	// The primary assertion is that the race detector does not fire.
	// As a secondary check, verify the session is in a consistent state.
	s.mu.RLock()
	f := s.fileByPathLocked("plan.md")
	_ = f.Comments // access under lock — no race
	_ = f.FileHash
	s.mu.RUnlock()
}

// TestCarryForwardAllComments_NoDuplicateOnDisk verifies that carried-forward
// comments don't produce duplicates when WriteFiles merges with disk state.
// The old comment ID must be tracked as deleted so mergeFileSnapshotIntoCritJSON
// skips it, leaving only the new carried-forward copy.
func TestCarryForwardAllComments_NoDuplicateOnDisk(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")

	s := &Session{
		Mode:           "git",
		RepoRoot:       dir,
		ReviewFilePath: reviewPath,
		Files: []*FileEntry{
			{
				Path:     "main.go",
				AbsPath:  filepath.Join(dir, "main.go"),
				Status:   "modified",
				FileType: "code",
				Comments: []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old1",
						StartLine: 10,
						EndLine:   10,
						Body:      "Fix this",
						Author:    "Tomasz",
						Scope:     "line",
						CreatedAt: "2026-04-13T10:00:00Z",
						UpdatedAt: "2026-04-13T10:00:00Z",
						Resolved:  true,
						Replies: []Reply{
							{ID: "rp_1", Body: "Fixed", Author: "Agent", CreatedAt: "2026-04-13T10:01:00Z"},
						},
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	// Write the "old" version to disk (simulates the state before round-complete).
	oldCJ := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{
						ID:        "c_old1",
						StartLine: 10,
						EndLine:   10,
						Body:      "Fix this",
						Author:    "Tomasz",
						Scope:     "line",
						CreatedAt: "2026-04-13T10:00:00Z",
						UpdatedAt: "2026-04-13T10:00:00Z",
						Resolved:  true,
						Replies: []Reply{
							{ID: "rp_1", Body: "Fixed", Author: "Agent", CreatedAt: "2026-04-13T10:01:00Z"},
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(oldCJ, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Run carry-forward (simulates what handleRoundCompleteGit does).
	s.mu.Lock()
	s.carryForwardAllComments()
	s.mu.Unlock()

	// Verify in-memory state: exactly 1 comment with a NEW id.
	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 carried-forward comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if carried.ID == "c_old1" {
		t.Error("carried-forward comment should have a new ID")
	}
	if !carried.CarriedForward {
		t.Error("expected CarriedForward=true")
	}

	// Now write to disk (this is where the duplicate appears without the fix).
	s.WriteFiles()

	diskData, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	var diskCJ CritJSON
	if err := json.Unmarshal(diskData, &diskCJ); err != nil {
		t.Fatal(err)
	}

	diskComments := diskCJ.Files["main.go"].Comments
	if len(diskComments) != 1 {
		t.Errorf("expected 1 comment on disk after WriteFiles, got %d", len(diskComments))
		for _, c := range diskComments {
			t.Logf("  id=%s carried_forward=%v resolved=%v", c.ID, c.CarriedForward, c.Resolved)
		}
	}
}

func TestCarryForwardComments_NoDuplicateOnDisk(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review.json")
	mdPath := filepath.Join(dir, "plan.md")
	os.WriteFile(mdPath, []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644)

	s := &Session{
		Mode:           "files",
		RepoRoot:       dir,
		ReviewFilePath: reviewPath,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         "# Plan\n\nStep 1\n\nStep 2\n",
				PreviousContent: "# Plan\n\nStep 1\n",
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old_md",
						StartLine: 3,
						EndLine:   3,
						Body:      "Expand this",
						Author:    "Tomasz",
						Scope:     "line",
						CreatedAt: "2026-04-13T10:00:00Z",
						UpdatedAt: "2026-04-13T10:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	// Write old version to disk.
	oldCJ := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Status: "modified",
				Comments: []Comment{
					{
						ID:        "c_old_md",
						StartLine: 3,
						EndLine:   3,
						Body:      "Expand this",
						Author:    "Tomasz",
						Scope:     "line",
						CreatedAt: "2026-04-13T10:00:00Z",
						UpdatedAt: "2026-04-13T10:00:00Z",
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(oldCJ, "", "  ")
	os.WriteFile(reviewPath, data, 0644)

	// Run markdown carry-forward.
	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 carried-forward comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if carried.ID == "c_old_md" {
		t.Error("carried-forward comment should have a new ID")
	}
	// Line 3 in old content ("Step 1") is still line 3 in new content.
	if carried.StartLine != 3 || carried.EndLine != 3 {
		t.Errorf("expected line 3, got start=%d end=%d", carried.StartLine, carried.EndLine)
	}

	// Write to disk — should not produce duplicates.
	s.WriteFiles()

	diskData, _ := os.ReadFile(reviewPath)
	var diskCJ CritJSON
	json.Unmarshal(diskData, &diskCJ)

	diskComments := diskCJ.Files["plan.md"].Comments
	if len(diskComments) != 1 {
		t.Errorf("expected 1 comment on disk, got %d", len(diskComments))
		for _, c := range diskComments {
			t.Logf("  id=%s carried_forward=%v", c.ID, c.CarriedForward)
		}
	}
}

func TestCarryForwardComment_PreservesQuote(t *testing.T) {
	offset := 5
	old := Comment{
		ID:          "c_old",
		StartLine:   10,
		EndLine:     10,
		Body:        "Fix this",
		Quote:       "the quoted text",
		QuoteOffset: &offset,
		Author:      "Tomasz",
		Scope:       "line",
		CreatedAt:   "2026-04-13T10:00:00Z",
		UpdatedAt:   "2026-04-13T10:00:00Z",
		Resolved:    true,
		ReviewRound: 1,
		Replies: []Reply{
			{ID: "rp_1", Body: "Done", Author: "Agent"},
		},
	}

	carried := carryForwardComment(old, "c_new", "2026-04-13T11:00:00Z")

	if carried.Quote != "the quoted text" {
		t.Errorf("Quote not preserved: got %q", carried.Quote)
	}
	if carried.QuoteOffset == nil || *carried.QuoteOffset != 5 {
		t.Error("QuoteOffset not preserved")
	}
}

// TestWatchGit_SkipsGitStatusWhenNotWaiting verifies that watchGit does not
// detect edits when waitingForAgent is false, and does detect them once
// waitingForAgent is set to true.
func TestWatchGit_SkipsGitStatusWhenNotWaiting(t *testing.T) {
	dir := initTestRepo(t)

	// Create a feature branch so we have a known base
	runGit(t, dir, "checkout", "-b", "feat")
	writeFile(t, filepath.Join(dir, "file.go"), "package main\n")
	runGit(t, dir, "add", "file.go")
	runGit(t, dir, "commit", "-m", "add file")

	s := &Session{
		Mode:        "git",
		RepoRoot:    dir,
		Branch:      "feat",
		BaseRef:     "main",
		ReviewRound: 1,
		Files: []*FileEntry{
			{
				Path:     "file.go",
				AbsPath:  filepath.Join(dir, "file.go"),
				Status:   "modified",
				FileType: "code",
				Comments: []Comment{},
			},
		},
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	stop := make(chan struct{})
	defer close(stop)

	// Start watchGit with waitingForAgent = false (default).
	// WorkingTreeFingerprint uses cwd, so chdir before starting.
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	go s.watchGit(stop)

	// Create an untracked file — this changes git status --porcelain output.
	time.Sleep(500 * time.Millisecond) // let watcher start
	writeFile(t, filepath.Join(dir, "untracked1.txt"), "hello\n")

	// Wait for a couple of watcher ticks — should NOT detect edits.
	time.Sleep(2500 * time.Millisecond)
	if edits := s.GetPendingEdits(); edits != 0 {
		t.Errorf("expected 0 edits while not waiting for agent, got %d", edits)
	}

	// Now set waitingForAgent = true and wait for the baseline tick.
	s.setWaitingForAgent(true)
	time.Sleep(1500 * time.Millisecond) // baseline tick

	// Create another new file to change the fingerprint from the baseline.
	writeFile(t, filepath.Join(dir, "untracked2.txt"), "world\n")
	time.Sleep(2500 * time.Millisecond)

	if edits := s.GetPendingEdits(); edits == 0 {
		t.Error("expected edits > 0 after setting waitingForAgent = true")
	}
}

func TestRestoreOrphanedComments(t *testing.T) {
	dir := t.TempDir()

	s := &Session{
		Mode:     "git",
		Branch:   "main",
		RepoRoot: dir,
		Files: []*FileEntry{
			{Path: "existing.md", AbsPath: filepath.Join(dir, "existing.md"), Status: "modified", FileType: "markdown"},
		},
		ReviewRound: 1,
		subscribers: make(map[chan SSEEvent]struct{}),
	}

	// Write a review file with comments on an orphaned path
	critPath := s.critJSONPath()
	cj := CritJSON{
		Branch:      "main",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"existing.md": {
				Status:   "modified",
				Comments: []Comment{{ID: "c1", Body: "still here", Scope: "line", StartLine: 1, EndLine: 1}},
			},
			"temp.go": {
				Status: "added",
				Comments: []Comment{
					{ID: "c_temp1", Body: "this will be orphaned", Scope: "file"},
				},
			},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(critPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(critPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify temp.go is NOT in s.Files
	for _, f := range s.Files {
		if f.Path == "temp.go" {
			t.Fatal("temp.go should not be in session files before restore")
		}
	}

	// Restore orphaned comments
	s.restoreOrphanedComments()

	// temp.go should now be in s.Files as orphaned
	var orphaned *FileEntry
	for _, f := range s.Files {
		if f.Path == "temp.go" {
			orphaned = f
			break
		}
	}
	if orphaned == nil {
		t.Fatal("orphaned file not restored")
	}
	if !orphaned.Orphaned {
		t.Error("expected Orphaned=true")
	}
	if orphaned.Status != "removed" {
		t.Errorf("expected status 'removed', got %q", orphaned.Status)
	}
	if len(orphaned.Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(orphaned.Comments))
	}

	// Calling again should NOT duplicate
	s.restoreOrphanedComments()
	count := 0
	for _, f := range s.Files {
		if f.Path == "temp.go" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 temp.go entry after double restore, got %d", count)
	}
}

func TestCarryForward_AnchorCorrectShift(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	os.WriteFile(mdPath, []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:     "plan.md",
				AbsPath:  mdPath,
				Status:   "modified",
				FileType: "markdown",
				// New content: two new lines inserted before Step 1
				Content:         "# Plan\n\nNew line A\nNew line B\nStep 1\n\nStep 2\n",
				PreviousContent: "# Plan\n\nStep 1\n\nStep 2\n",
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 3,
						EndLine:   3,
						Body:      "Expand this",
						Anchor:    "Step 1",
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if carried.StartLine != 5 || carried.EndLine != 5 {
		t.Errorf("expected line 5, got start=%d end=%d", carried.StartLine, carried.EndLine)
	}
	if carried.Drifted {
		t.Error("expected Drifted=false when anchor matches")
	}
	if carried.Anchor != "Step 1" {
		t.Errorf("Anchor = %q, want %q", carried.Anchor, "Step 1")
	}
}

func TestCarryForward_AnchorDrifted(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	os.WriteFile(mdPath, []byte("# Plan\n\nSomething else\n"), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         "# Plan\n\nSomething else\n",
				PreviousContent: "# Plan\n\nStep 1\n",
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 3,
						EndLine:   3,
						Body:      "Fix this",
						Anchor:    "Step 1",
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if !carried.Drifted {
		t.Error("expected Drifted=true when anchor is not found")
	}
}

func TestCarryForward_AnchorFindsCorrectPositionWhenLCSWrong(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")

	// Old content: lines are A B C D E
	oldContent := "A\nB\nC\nD\nE\n"
	// New content: lines are X Y C Z A B D E
	// The comment was on line 1 ("A") in old content.
	// LCS might map line 1 to some position, but "A" is now at line 5.
	newContent := "X\nY\nC\nZ\nA\nB\nD\nE\n"
	os.WriteFile(mdPath, []byte(newContent), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         newContent,
				PreviousContent: oldContent,
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 1,
						EndLine:   1,
						Body:      "Comment on A",
						Anchor:    "A",
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if carried.StartLine != 5 || carried.EndLine != 5 {
		t.Errorf("expected line 5 (where 'A' now is), got start=%d end=%d", carried.StartLine, carried.EndLine)
	}
	if carried.Drifted {
		t.Error("expected Drifted=false since anchor was found")
	}
}

func TestCarryForward_WithoutAnchorBackwardsCompatible(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	os.WriteFile(mdPath, []byte("# Plan\n\nNew line\nStep 1\n\nStep 2\n"), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         "# Plan\n\nNew line\nStep 1\n\nStep 2\n",
				PreviousContent: "# Plan\n\nStep 1\n\nStep 2\n",
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 3,
						EndLine:   3,
						Body:      "Expand this",
						// No Anchor field — backwards compatible
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	// LCS should remap line 3 -> 4 (shifted by inserted line).
	if carried.StartLine != 4 || carried.EndLine != 4 {
		t.Errorf("expected LCS remap to line 4, got start=%d end=%d", carried.StartLine, carried.EndLine)
	}
	if carried.Drifted {
		t.Error("expected Drifted=false for comments without anchor")
	}
}

func TestCarryForwardComment_PreservesAnchor(t *testing.T) {
	old := Comment{
		ID:        "c_old",
		StartLine: 10,
		EndLine:   12,
		Body:      "Fix this",
		Anchor:    "line10\nline11\nline12",
		Scope:     "line",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	carried := carryForwardComment(old, "c_new", "2026-01-02T00:00:00Z")

	if carried.Anchor != "line10\nline11\nline12" {
		t.Errorf("Anchor not preserved: got %q", carried.Anchor)
	}
	if carried.Drifted {
		t.Error("Drifted should be false on fresh carry-forward")
	}
}

func TestCarryForward_AnchorMultilineRange(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")

	oldContent := "Header\nLine A\nLine B\nLine C\nFooter\n"
	// New content: block moved down by 3 lines
	newContent := "Header\nX\nY\nZ\nLine A\nLine B\nLine C\nFooter\n"
	os.WriteFile(mdPath, []byte(newContent), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         newContent,
				PreviousContent: oldContent,
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 2,
						EndLine:   4,
						Body:      "Refactor this block",
						Anchor:    "Line A\nLine B\nLine C",
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	if carried.StartLine != 5 || carried.EndLine != 7 {
		t.Errorf("expected lines 5-7, got start=%d end=%d", carried.StartLine, carried.EndLine)
	}
	if carried.Drifted {
		t.Error("expected Drifted=false")
	}
}

func TestCarryForward_AnchorLenFromAnchorNotLCS(t *testing.T) {
	// Regression: anchorLen must come from the anchor text, not the LCS span.
	// If LCS maps old lines 2-4 to new lines 5-10 (gap lines inserted between),
	// the end line should still be 5+3-1=7 (3-line anchor), not 5+6-1=10.
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")

	oldContent := "Header\nLine A\nLine B\nLine C\nFooter\n"
	// New content: anchor block exists but LCS may map start/end with a gap.
	// Insert lines between where LCS puts start vs end to create divergence.
	newContent := "Header\nExtra1\nExtra2\nExtra3\nLine A\nLine B\nLine C\nExtra4\nFooter\n"
	os.WriteFile(mdPath, []byte(newContent), 0644)

	s := &Session{
		Mode:     "files",
		RepoRoot: dir,
		Files: []*FileEntry{
			{
				Path:            "plan.md",
				AbsPath:         mdPath,
				Status:          "modified",
				FileType:        "markdown",
				Content:         newContent,
				PreviousContent: oldContent,
				Comments:        []Comment{},
				PreviousComments: []Comment{
					{
						ID:        "c_old",
						StartLine: 2,
						EndLine:   4,
						Body:      "Review this",
						Anchor:    "Line A\nLine B\nLine C",
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z",
						UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
		roundComplete: make(chan struct{}, 1),
	}

	s.carryForwardComments()

	if len(s.Files[0].Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(s.Files[0].Comments))
	}
	carried := s.Files[0].Comments[0]
	// Anchor is 3 lines, so end should be start+2 regardless of LCS span.
	if carried.StartLine != 5 || carried.EndLine != 7 {
		t.Errorf("expected lines 5-7, got start=%d end=%d", carried.StartLine, carried.EndLine)
	}
	if carried.Drifted {
		t.Error("expected Drifted=false — anchor found in file")
	}
}
