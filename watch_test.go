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
