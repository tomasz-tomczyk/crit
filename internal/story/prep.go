package story

import (
	"fmt"
	"strings"
)

// PrepHunk is one diff hunk for prep-text rendering. Body is the raw hunk text
// (the lines under the @@ header); it is emitted verbatim and untrimmed.
type PrepHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Header   string // the @@ line
	Body     string // hunk lines under the header
}

// PrepFile is a changed file with its hunks for prep rendering.
type PrepFile struct {
	Path   string
	Status string // "modified", "added", ...
	Hunks  []PrepHunk
}

// PrepInput is everything BuildPrep needs, decoupled from the session/diff
// engine so it can be built from a live git session or a test fixture.
type PrepInput struct {
	BaseSHA        string
	HeadSHA        string // empty for working-tree scopes
	CommitMessages []string
	Files          []PrepFile
}

// Prep is the result of building prep text: the stage-cli-style text to hand
// to the agent, the scope snapshot fields to record on the story, and the
// indexed hunk IDs (for ingest coverage math).
type Prep struct {
	Text             string
	BaseSHA          string
	HeadSHA          string
	ScopeFingerprint string
	Indexed          []HunkID
}

// BuildPrep renders the full, untrimmed stage-cli-style prep text from the diff
// scope and snapshots BaseSHA/HeadSHA/ScopeFingerprint. There is deliberately
// no diff budget: the agent reads the whole thing (prompt-by-reference, §4.3),
// so every hunk is visible and the coverage contract stays satisfiable at any
// diff size.
func BuildPrep(in PrepInput) Prep {
	var hunks strings.Builder
	var indexed []HunkID

	hunks.WriteString("=== HUNKS ===\n")
	for _, f := range in.Files {
		for _, h := range f.Hunks {
			id := HunkID{FilePath: f.Path, OldStart: h.OldStart}
			indexed = append(indexed, id)

			// Hunk id line: (filePath, oldStart) + status, so the agent can
			// reference hunks by the same (file_path, old_start) scheme the
			// StoryHunkRef schema uses. New files use oldStart 0.
			hunks.WriteString(fmt.Sprintf("--- %s [%s]\n", id.String(), f.Status))
			writeLine(&hunks, h.Header)
			writeLine(&hunks, h.Body)
			hunks.WriteString("\n")
		}
	}

	fingerprint := Fingerprint(indexed)

	// The prep text opens with the scope snapshot so an author reading the prep
	// file (via --prep) can copy base_sha / head_sha / scope_fingerprint into
	// their story JSON — ingest re-derives the fingerprint and rejects on drift.
	var b strings.Builder
	b.WriteString("=== SCOPE ===\n")
	b.WriteString(fmt.Sprintf("base_sha: %s\n", in.BaseSHA))
	b.WriteString(fmt.Sprintf("head_sha: %s\n", in.HeadSHA))
	b.WriteString(fmt.Sprintf("scope_fingerprint: %s\n\n", fingerprint))
	if len(in.CommitMessages) > 0 {
		b.WriteString("=== COMMITS ===\n")
		for _, m := range in.CommitMessages {
			b.WriteString(m)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(hunks.String())

	return Prep{
		Text:             b.String(),
		BaseSHA:          in.BaseSHA,
		HeadSHA:          in.HeadSHA,
		ScopeFingerprint: fingerprint,
		Indexed:          indexed,
	}
}

// writeLine writes s followed by a newline, unless s is empty or already ends
// with one.
func writeLine(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	b.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		b.WriteString("\n")
	}
}
