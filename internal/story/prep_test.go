package story

import (
	"strings"
	"testing"
)

func TestBuildPrep_HunkSectionsAndIDs(t *testing.T) {
	in := PrepInput{
		BaseSHA:        "base123",
		HeadSHA:        "head456",
		CommitMessages: []string{"Add auth middleware", "Wire it into the router"},
		Files: []PrepFile{
			{
				Path:   "auth/mw.go",
				Status: "modified",
				Hunks: []PrepHunk{
					{OldStart: 12, OldCount: 3, NewStart: 12, NewCount: 6, Header: "@@ -12,3 +12,6 @@ func Auth()", Body: "+	log.Println(\"in\")\n context line\n"},
				},
			},
			{
				Path:   "auth/new.go",
				Status: "added",
				Hunks: []PrepHunk{
					{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 4, Header: "@@ -0,0 +1,4 @@", Body: "+package auth\n"},
				},
			},
		},
	}

	prep := BuildPrep(in)

	// Commit messages appear up top.
	if !strings.Contains(prep.Text, "Add auth middleware") {
		t.Errorf("prep text missing commit message:\n%s", prep.Text)
	}
	// A HUNKS section delimiter is present.
	if !strings.Contains(prep.Text, "=== HUNKS ===") {
		t.Errorf("prep text missing === HUNKS === delimiter:\n%s", prep.Text)
	}
	// Hunk IDs are (filePath, oldStart) — new files use oldStart 0.
	if !strings.Contains(prep.Text, "(auth/mw.go, 12)") {
		t.Errorf("prep text missing modified-file hunk id:\n%s", prep.Text)
	}
	if !strings.Contains(prep.Text, "(auth/new.go, 0)") {
		t.Errorf("prep text missing added-file hunk id (oldStart 0):\n%s", prep.Text)
	}
	// Old/new line numbers surface via the @@ header.
	if !strings.Contains(prep.Text, "@@ -12,3 +12,6 @@") {
		t.Errorf("prep text missing hunk header:\n%s", prep.Text)
	}
}

func TestBuildPrep_ScopeHeaderSnapshot(t *testing.T) {
	in := PrepInput{
		BaseSHA: "base123",
		HeadSHA: "head456",
		Files:   []PrepFile{{Path: "a.go", Hunks: []PrepHunk{{OldStart: 1, Header: "@@ -1 +1 @@"}}}},
	}
	prep := BuildPrep(in)
	// The scope snapshot is emitted up top so an author can copy it verbatim.
	for _, want := range []string{"=== SCOPE ===", "base_sha: base123", "head_sha: head456", "scope_fingerprint: " + prep.ScopeFingerprint} {
		if !strings.Contains(prep.Text, want) {
			t.Errorf("prep text missing %q:\n%s", want, prep.Text)
		}
	}
}

func TestBuildPrep_FingerprintOverIndexedHunks(t *testing.T) {
	in := PrepInput{
		BaseSHA: "b",
		HeadSHA: "h",
		Files: []PrepFile{
			{Path: "a.go", Hunks: []PrepHunk{{OldStart: 1, Header: "@@ -1 +1 @@"}}},
			{Path: "b.go", Hunks: []PrepHunk{{OldStart: 5, Header: "@@ -5 +5 @@"}}},
		},
	}
	prep := BuildPrep(in)

	// The fingerprint must equal Fingerprint over the same indexed hunk IDs,
	// so ingest's recomputed live fingerprint matches on an unchanged tree.
	want := Fingerprint([]HunkID{{FilePath: "a.go", OldStart: 1}, {FilePath: "b.go", OldStart: 5}})
	if prep.ScopeFingerprint != want {
		t.Errorf("ScopeFingerprint = %q, want %q", prep.ScopeFingerprint, want)
	}
	if prep.BaseSHA != "b" || prep.HeadSHA != "h" {
		t.Errorf("scope SHAs not snapshotted: %+v", prep)
	}
	// Indexed hunk IDs are exposed for the caller to hand to ingest.
	if len(prep.Indexed) != 2 {
		t.Fatalf("expected 2 indexed hunks, got %d", len(prep.Indexed))
	}
}

func TestBuildPrep_HeaderIncludesFilePathAndStatus(t *testing.T) {
	in := PrepInput{
		Files: []PrepFile{
			{Path: "x/y.go", Status: "modified", Hunks: []PrepHunk{{OldStart: 3, Header: "@@ -3 +3 @@"}}},
		},
	}
	prep := BuildPrep(in)
	if !strings.Contains(prep.Text, "x/y.go") {
		t.Errorf("prep text missing file path:\n%s", prep.Text)
	}
	if !strings.Contains(prep.Text, "modified") {
		t.Errorf("prep text missing file status:\n%s", prep.Text)
	}
}
