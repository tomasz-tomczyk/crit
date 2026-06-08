package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Migration gaps ---

// TestMigrate_LeftoverTmpDir simulates a crash mid-migration that left a
// previous attempt's <id>.crit-migrate.tmp/ behind. The next migration must
// wipe it and complete successfully rather than failing because MkdirAll
// preserves the stale tmp contents.
func TestMigrate_LeftoverTmpDir(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	tmp := identity + ".crit-migrate.tmp"

	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale junk inside the tmp dir from a prior crashed run.
	if err := os.WriteFile(filepath.Join(tmp, "stale.txt"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identity, []byte(`{"branch":"main","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("migration with leftover tmp dir failed: %v", err)
	}
	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatalf("identity not folder after migration: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(identity, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale tmp content leaked into final folder: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp dir not cleaned up: %v", err)
	}
}

// TestMigrate_MalformedFlatFile_StillMigrates verifies that the migration
// shim is structural (rename), not parsing — a flat file with garbage JSON
// is still moved into the folder layout. Subsequent loadCritJSON will surface
// the parse error to the user.
func TestMigrate_MalformedFlatFile_StillMigrates(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.WriteFile(identity, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("migration aborted on malformed JSON: %v", err)
	}
	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatalf("identity not folder: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(identity, "review.json"))
	if err != nil {
		t.Fatalf("review.json missing after migration: %v", err)
	}
	if string(data) != "{not valid json" {
		t.Errorf("file content corrupted by migration: %q", data)
	}
}

// TestMigrate_LegacyDotJSONFlatFile covers the v3-on-disk shape where the
// review file lives at <key>.json (with extension) under ~/.crit/reviews/.
// The v4 identity drops the extension; ensureReviewFolder must detect the
// sibling flat file and migrate it into <key>/review.json.
func TestMigrate_LegacyDotJSONFlatFile(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	legacy := identity + ".json"
	if err := os.WriteFile(legacy, []byte(`{"branch":"v3","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// v3 sidecar (unlikely shipped, but the migration handles it).
	if err := os.WriteFile(legacy+".snapshots.json", []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("migration: %v", err)
	}
	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatalf("v4 identity %s is not a folder: %v", identity, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy %s not removed: %v", legacy, err)
	}
	cj, err := loadCritJSON(identity)
	if err != nil {
		t.Fatalf("loadCritJSON: %v", err)
	}
	if cj.Branch != "v3" {
		t.Errorf("review payload lost: %+v", cj)
	}
	if _, err := os.Stat(filepath.Join(identity, "snapshots.json")); err != nil {
		t.Errorf("sidecar not moved into folder: %v", err)
	}
}

// TestMigrate_LegacyDotJSONFolder covers the early-v4 mid-state where the
// review folder was created with a stray .json extension on its name. The
// next ensureReviewFolder call must rename <key>.json/ to <key>/.
func TestMigrate_LegacyDotJSONFolder(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	legacy := identity + ".json"
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "review.json"), []byte(`{"branch":"midstate","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy folder %s still present: %v", legacy, err)
	}
	cj, err := loadCritJSON(identity)
	if err != nil {
		t.Fatalf("loadCritJSON: %v", err)
	}
	if cj.Branch != "midstate" {
		t.Errorf("review payload lost across folder rename: %+v", cj)
	}
}

// TestMigrate_InRepoCritJSON migrates an in-repo /tmp/.../path/.crit.json
// (the OutputDir-based identity) to the v4 folder layout. Mirrors the
// real-world path where a user has --output set to a project subdirectory.
func TestMigrate_InRepoCritJSON(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, ".crit")
	if err := os.WriteFile(identity, []byte(`{"branch":"feat","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureReviewFolder(identity); err != nil {
		t.Fatalf("migration: %v", err)
	}
	info, err := os.Stat(identity)
	if err != nil || !info.IsDir() {
		t.Fatalf(".crit.json identity not folder: %v", err)
	}
	got, err := loadCritJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "feat" {
		t.Errorf("round-trip lost: %+v", got)
	}
}

// --- Folder layout: future attachments ---

// TestSaveCritJSON_PreservesUnknownAttachments writes review.json into a
// folder that already contains a non-crit attachment file. The save must
// not delete or overwrite siblings — the folder is the per-review namespace
// for future features (recordings, large diffs, etc).
func TestSaveCritJSON_PreservesUnknownAttachments(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}
	attach := filepath.Join(identity, "attachment.bin")
	if err := os.WriteFile(attach, []byte("opaque"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveCritJSON(identity, CritJSON{Branch: "main", Files: map[string]CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(attach); err != nil || string(data) != "opaque" {
		t.Fatalf("attachment lost or corrupted: data=%q err=%v", data, err)
	}
}

// TestWriteFiles_EmptyPreservesAttachments is a defense-in-depth follow-up to
// the existing sidecar regression test: when WriteFiles takes the empty branch
// and removes review.json, future-attachment siblings must also survive.
func TestWriteFiles_EmptyPreservesAttachments(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionFromFiles([]string{planPath}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}
	identity := filepath.Join(dir, ".crit")
	s.ReviewFilePath = identity
	paths := reviewPathsFor(identity)

	if err := os.MkdirAll(paths.Folder, 0o755); err != nil {
		t.Fatal(err)
	}
	attach := filepath.Join(paths.Folder, "future.bin")
	if err := os.WriteFile(attach, []byte("future"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveCritJSON(identity, CritJSON{Branch: s.Branch, Files: map[string]CritJSONFile{}}); err != nil {
		t.Fatal(err)
	}

	flushWrites(s)
	s.WriteFiles()

	if _, err := os.Stat(paths.Review); !os.IsNotExist(err) {
		t.Errorf("review.json should be removed: %v", err)
	}
	if data, err := os.ReadFile(attach); err != nil || string(data) != "future" {
		t.Errorf("attachment lost across empty WriteFiles: data=%q err=%v", data, err)
	}
}

// --- Cleanup gaps ---

// TestFindStaleReviews_OrphanFolder_BoundaryNotStale verifies that an orphan
// snapshots-only folder whose mtime is JUST inside the cutoff (newer than
// cutoff) is NOT collected. Boundary: orphan with mtime exactly equal to
// cutoff is also not stale (the code uses Before, not !After).
func TestFindStaleReviews_OrphanFolder_BoundaryNotStale(t *testing.T) {
	dir := t.TempDir()
	orphan := filepath.Join(dir, "orphan")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "snapshots.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Folder mtime is 1 day old; cutoff is 7 days. Should NOT be stale.
	recent := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(orphan, recent, recent); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	for _, s := range stale {
		if s.path == orphan {
			t.Fatalf("recent orphan folder must not be collected: %+v", s)
		}
	}
}

// TestFindStaleReviews_EmptyFolder_Skipped: a folder inside reviews/ with
// neither review.json NOR snapshots.json (e.g. a partial mkdir from a crashed
// constructor) must be ignored. checkStaleReviewFolder returns false.
func TestFindStaleReviews_EmptyFolder_Skipped(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "emptyfolder")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(empty, old, old); err != nil {
		t.Fatal(err)
	}
	stale := findStaleReviews(dir, 7)
	for _, s := range stale {
		if s.path == empty {
			t.Fatalf("empty folder must not be considered stale: %+v", s)
		}
	}
}

// TestFindStaleReviews_MixedFolderAndFlat verifies the migration-removal
// branch: a v3 flat .json next to a v4 folder, both stale, are both
// collected. After the shim is deleted the flat half goes away.
func TestFindStaleReviews_MixedFolderAndFlat(t *testing.T) {
	dir := t.TempDir()
	oldTime := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)

	// v4 folder.
	folder := filepath.Join(dir, "k1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{Branch: "old1", UpdatedAt: oldTime, Files: map[string]CritJSONFile{}}
	data, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(filepath.Join(folder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// v3 flat.
	flat := filepath.Join(dir, "k2.json")
	cj2 := CritJSON{Branch: "old2", UpdatedAt: oldTime, Files: map[string]CritJSONFile{}}
	data2, _ := json.MarshalIndent(cj2, "", "  ")
	if err := os.WriteFile(flat, data2, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := findStaleReviews(dir, 7)
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale (folder + flat), got %d: %+v", len(stale), stale)
	}
	branches := map[string]bool{}
	for _, s := range stale {
		branches[s.branch] = true
	}
	if !branches["old1"] || !branches["old2"] {
		t.Errorf("missing branch: %+v", branches)
	}
}

// TestDeleteStaleReviews_PartialFailureContinues: when one entry's path is
// missing on disk, the sweep must continue and delete the remaining entries
// rather than aborting at the first failure. removeStaleReviewPath returns
// false on missing path; deleteStaleReviews logs and continues.
func TestDeleteStaleReviews_PartialFailureContinues(t *testing.T) {
	dir := t.TempDir()
	// Real folder.
	good := filepath.Join(dir, "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "review.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing-never-created")

	// Hide stderr to avoid polluting test output.
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer devnull.Close()
	prev := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = prev }()

	deleted := deleteStaleReviews([]staleReview{
		{key: "missing", path: missing},
		{key: "good", path: good},
	})
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (missing skipped, good removed)", deleted)
	}
	if _, err := os.Stat(good); !os.IsNotExist(err) {
		t.Error("good folder not removed after partial-failure sweep")
	}
}

// TestCleanupOnApproval_FlatFileFallback exercises the MIGRATION-REMOVAL
// branch in removeStaleReviewPath: an unmigrated flat .json review with a
// sibling .snapshots.json file. Both must be cleaned.
func TestCleanupOnApproval_FlatFileFallback(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(identity, []byte(`{"branch":"main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecar := identity + ".snapshots.json"
	if err := os.WriteFile(sidecar, []byte(`{"round_snapshots":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupOnApproval(true, identity, true)

	if _, err := os.Stat(identity); !os.IsNotExist(err) {
		t.Errorf("flat review not removed: %v", err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("flat sidecar not removed: %v", err)
	}
}

// --- walkReviewIdentities / find-by-id gaps ---

// TestFindReviewFileByCommentID_AmbiguousAcrossFolderAndFlat: if the same
// comment ID lives in both a folder and a leftover flat file (test-pollution
// scenario or an aborted migration in production), the function must return
// an error rather than silently picking one.
func TestFindReviewFileByCommentID_AmbiguousAcrossFolderAndFlat(t *testing.T) {
	setHome(t, t.TempDir())
	dir, err := reviewsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	commentID := "c_dup_xyz"
	cj := CritJSON{
		Branch: "main",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{{ID: commentID, Body: "x"}}},
		},
	}
	data, _ := json.Marshal(cj)

	// Folder-form review with the comment.
	folder := filepath.Join(dir, "k1")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "review.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Legacy flat-file with the same comment.
	flat := filepath.Join(dir, "k2.json")
	if err := os.WriteFile(flat, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = findReviewFileByCommentID(commentID, "")
	if err == nil {
		t.Fatal("expected ambiguity error when comment exists in both folder and flat review")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error should mention multiple matches, got: %v", err)
	}
}

// TestWalkReviewIdentities_SkipsOrphanFolderSilently: a folder with only
// snapshots.json (no review.json) is benignly skipped — the visit callback
// never runs for it. This is the contract the find-by-id/branch helpers rely
// on after the v4 layout shipped.
func TestWalkReviewIdentities_SkipsOrphanFolderSilently(t *testing.T) {
	setHome(t, t.TempDir())
	dir, _ := reviewsDir()
	if err := os.MkdirAll(filepath.Join(dir, "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orphan", "snapshots.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "review.json"), []byte(`{"branch":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var visited []string
	err := walkReviewIdentities(func(identity string, _ []byte) error {
		visited = append(visited, filepath.Base(identity))
		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	for _, v := range visited {
		if v == "orphan" {
			t.Errorf("orphan folder should not be visited: %v", visited)
		}
	}
	found := false
	for _, v := range visited {
		if v == "good" {
			found = true
		}
	}
	if !found {
		t.Errorf("good folder should be visited, got: %v", visited)
	}
}

// TestFindReviewFileByBranch_MalformedJSONSkipped: a corrupt review.json
// inside the reviews dir must not abort the scan; its identity is silently
// skipped (via the //nolint:nilerr branch) and a sibling valid review still
// matches.
func TestFindReviewFileByBranch_MalformedJSONSkipped(t *testing.T) {
	setHome(t, t.TempDir())
	dir, _ := reviewsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "review.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := writeFolderReviewFixture(t, "good", "feature-x")

	got, err := findReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatalf("scan should succeed despite corrupt sibling: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- API edge cases ---

// TestHandleFile_RoundZero rejects ?round=0 with 400 (round is 1-indexed).
func TestHandleFile_RoundZero(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=0", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("?round=0 should be 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleFile_RoundCurrentMatchesSnapshot: ?round=N where N == current
// round returns the recorded snapshot, which must match in-memory current
// content for files present at that round.
func TestHandleFile_RoundCurrentMatchesSnapshot(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	wantContent := sess.RoundSnapshots["test.md"][2].Content

	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got, _ := resp["content"].(string); got != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
}

// TestHandleFile_EmptyRoundParamFallsThrough: ?round= (empty value) is
// treated as "no round param" — the handler falls through to working-tree
// lookup rather than 400ing. This mirrors how Go's URL parser treats empty
// values and how the frontend passes "" to mean current.
func TestHandleFile_EmptyRoundParamFallsThrough(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("empty round should fall through to current, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleFile_DuplicateRoundParam_FirstWins: Go's url.Values.Get returns
// the first value for repeated query keys. Verify the behavior is stable —
// if a future code change switches to last-wins or errors, this catches it.
func TestHandleFile_DuplicateRoundParam_FirstWins(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	// round=1 first (valid), round=99 second (out of range).
	req := httptest.NewRequest("GET", "/api/file?path=test.md&round=1&round=99", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("first round=1 should win, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleRounds_GitMode returns an empty rounds list (200) for non-files
// sessions. The frontend renders no timeline when rounds is empty.
func TestHandleRounds_GitMode(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Mode = "git"
	req := httptest.NewRequest("GET", "/api/rounds", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var resp struct {
		Rounds []any `json:"rounds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Rounds) != 0 {
		t.Errorf("git mode must return empty rounds, got %d", len(resp.Rounds))
	}
}

// TestHandleRounds_MethodNotAllowed verifies non-GET methods are rejected.
func TestHandleRounds_MethodNotAllowed(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		req := httptest.NewRequest(m, "/api/rounds", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 405 {
			t.Errorf("%s /api/rounds: status=%d, want 405", m, w.Code)
		}
	}
}

// TestHandleFileComments_RoundFiltersByReviewRound: ?round=N on the comments
// list filters to comments whose ReviewRound <= N.
func TestHandleFileComments_RoundFiltersByReviewRound(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	sess.Files[0].Comments = []Comment{
		{ID: "c1", ReviewRound: 1, Body: "first round"},
		{ID: "c2", ReviewRound: 2, Body: "second round"},
		{ID: "c3", ReviewRound: 3, Body: "future"},
	}

	req := httptest.NewRequest("GET", "/api/file/comments?path=test.md&round=2", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got []Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 comments at round<=2, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.ReviewRound > 2 {
			t.Errorf("comment %q has ReviewRound=%d > 2", c.ID, c.ReviewRound)
		}
	}
}

// --- commentsAtOrBeforeRound edge cases ---

func TestCommentsAtOrBeforeRound_NegativeAndZero(t *testing.T) {
	cs := []Comment{{ID: "a", ReviewRound: 1}}
	if got := commentsAtOrBeforeRound(cs, 0); got != nil {
		t.Errorf("round=0 must return nil, got %v", got)
	}
	if got := commentsAtOrBeforeRound(cs, -5); got != nil {
		t.Errorf("round=-5 must return nil, got %v", got)
	}
}

func TestCommentsAtOrBeforeRound_DoesNotMutateInput(t *testing.T) {
	// commentsAtOrBeforeRound uses comments[:0:0] which forces append to
	// allocate a new backing array. Verify the input slice is not clobbered.
	cs := []Comment{
		{ID: "a", ReviewRound: 1},
		{ID: "b", ReviewRound: 5},
	}
	_ = commentsAtOrBeforeRound(cs, 1)
	if cs[1].ID != "b" {
		t.Errorf("input slice mutated: %+v", cs)
	}
}

// --- lineStatsForRound edge cases ---

// TestLineStatsForRound_FileAddedAtRoundN: a file with no R(N-1) snapshot
// but a snapshot at R(N) counts every line as an addition.
func TestLineStatsForRound_FileAddedAtRoundN(t *testing.T) {
	sess := &Session{
		Mode: "files",
		RoundSnapshots: map[string]map[int]RoundSnapshot{
			"new.md": {
				2: {Content: "a\nb\nc\n"},
			},
		},
	}
	adds, dels := lineStatsForRound(sess, 2)
	if adds == 0 {
		t.Errorf("file added at R2 should have non-zero adds, got %d/%d", adds, dels)
	}
	if dels != 0 {
		t.Errorf("file added at R2 should have 0 dels, got %d", dels)
	}
}

// TestLineStatsForRound_R1IsBaseline: R1 always returns 0/0 even when
// snapshots exist.
func TestLineStatsForRound_R1IsBaseline(t *testing.T) {
	sess := &Session{
		Mode: "files",
		RoundSnapshots: map[string]map[int]RoundSnapshot{
			"a.md": {1: {Content: "anything\n"}},
		},
	}
	adds, dels := lineStatsForRound(sess, 1)
	if adds != 0 || dels != 0 {
		t.Errorf("R1 stats must be 0/0, got %d/%d", adds, dels)
	}
}

// --- sessionStarted guard ---

// TestSession_LoadCritJSON_PostSetSessionIsNoOp verifies the lock-discipline
// guard: once Server.SetSession flips sessionStarted, calling loadCritJSON
// again is a no-op (logs to stderr, does not mutate session state). This
// protects against runtime data races on RoundSnapshots / reviewComments.
func TestSession_LoadCritJSON_PostSetSessionIsNoOp(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionFromFiles([]string{planPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(dir, ".crit")
	s.ReviewFilePath = identity

	// Pre-flag: snapshot some state, then set the flag (simulating
	// Server.SetSession), and seed disk with DIFFERENT state.
	s.sessionStarted.Store(0)
	s.reviewComments = []Comment{{ID: "in-mem", Body: "memory wins"}}

	if err := saveCritJSON(identity, CritJSON{
		Branch: "fromdisk",
		ReviewComments: []Comment{
			{ID: "from-disk", Body: "disk should NOT clobber"},
		},
		Files: map[string]CritJSONFile{},
	}); err != nil {
		t.Fatal(err)
	}

	s.sessionStarted.Store(1) // post-SetSession

	stderr := captureStderr(t, func() {
		s.loadCritJSON()
	})
	if !strings.Contains(stderr, "BUG: Session.loadCritJSON called post-SetSession") {
		t.Errorf("expected guard log on stderr, got: %q", stderr)
	}

	if len(s.reviewComments) != 1 || s.reviewComments[0].ID != "in-mem" {
		t.Errorf("post-flag loadCritJSON clobbered in-memory state: %+v", s.reviewComments)
	}
}

// --- Concurrency: race detector exercise ---

// TestRoundSnapshots_ConcurrentReadWrite hammers captureRoundSnapshot,
// availableRounds, and roundSnapshotForFile from multiple goroutines. The
// race detector is the assertion. Capture takes the role of the writer
// (under s.mu.Lock); availableRounds/roundSnapshotForFile take RLock.
func TestRoundSnapshots_ConcurrentReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race exercise in short mode")
	}
	s := &Session{
		Mode: "files",
		Files: []*FileEntry{
			{Path: "a.md", Status: "modified", Content: "v"},
			{Path: "b.md", Status: "modified", Content: "v"},
		},
	}

	const writers, readers, iters = 2, 4, 100
	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				s.mu.Lock()
				s.captureRoundSnapshot(i + 1)
				s.mu.Unlock()
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				s.mu.RLock()
				_ = s.availableRounds()
				_, _ = s.roundSnapshotForFile("a.md", 1)
				s.mu.RUnlock()
			}
		}()
	}
	wg.Wait()
}

// --- Backward compat: snapshot referencing files no longer in session ---

// TestLoadSnapshotsFromSidecar_OrphanedPathsKept: the sidecar may reference
// files that have since been removed from the session (file deleted from
// disk between rounds). The restore must keep the historical entries —
// they're the only record of past content — even though no live FileEntry
// references them. lineStatsForRound and /api/rounds gracefully ignore them.
func TestLoadSnapshotsFromSidecar_OrphanedPathsKept(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshotsFile(reviewPathsFor(identity).Snapshots, SnapshotsFile{
		RoundSnapshots: map[string]map[int]RoundSnapshot{
			"deleted-since.md": {1: {Content: "ghost"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	s := &Session{Mode: "files", Files: []*FileEntry{}}
	s.loadSnapshotsFromSidecar(identity)
	if _, ok := s.RoundSnapshots["deleted-since.md"][1]; !ok {
		t.Error("orphaned snapshot path was discarded; should be preserved for timeline")
	}
}

// TestLoadSnapshotsFromSidecar_UnknownFieldsTolerated: forward-compat —
// future versions may add fields to RoundSnapshot. Unknown fields must be
// ignored (encoding/json default), never error the load.
func TestLoadSnapshotsFromSidecar_UnknownFieldsTolerated(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "abc")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{
  "schema_version": 99,
  "round_snapshots": {
    "a.md": {"1": {"content": "x", "future_field": "ignored"}}
  }
}`
	if err := os.WriteFile(reviewPathsFor(identity).Snapshots, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Session{Mode: "files"}
	s.loadSnapshotsFromSidecar(identity)
	if got := s.RoundSnapshots["a.md"][1].Content; got != "x" {
		t.Errorf("content = %q, want %q (unknown fields must be ignored)", got, "x")
	}
}
