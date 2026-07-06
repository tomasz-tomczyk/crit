// Package story implements the ingest, coverage, and prep-text logic for
// crit's opt-in "story" review mode: an LLM-authored grouping of diff hunks
// into chapters. This package is the trust boundary — it validates and
// (per policy) repairs the editorial JSON before it is ever persisted.
package story

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// Reason sentinels written into StorySupportEntry.Reason by ingest.
const (
	ReasonAutoRepaired = "auto-repaired"
	ReasonIgnored      = "ignored"
	ReasonStub         = "stub"
)

// placedFloor is the minimum fraction of indexed hunks a story must place for
// omissions to be auto-repaired rather than rejected. Below it, the story is
// treated as drift or hallucination (§8).
const placedFloor = 0.5

// HunkID identifies a diff hunk by (file_path, old_start). old_start is 0 for
// new files (stage-cli convention). It is the ingest-side mirror of
// session.StoryHunkRef.
type HunkID struct {
	FilePath string
	OldStart int
}

func (h HunkID) String() string {
	return fmt.Sprintf("(%s, %d)", h.FilePath, h.OldStart)
}

func refID(r session.StoryHunkRef) HunkID {
	return HunkID{FilePath: r.FilePath, OldStart: r.OldStart}
}

func idRef(h HunkID) session.StoryHunkRef {
	return session.StoryHunkRef{FilePath: h.FilePath, OldStart: h.OldStart}
}

// Ingest is the input to Run: a parsed story plus the live diff scope it must
// be validated against.
type Ingest struct {
	// Story is the parsed editorial JSON. Run mutates it in place (support
	// back-fill, coverage) when it decides to save.
	Story *session.Story

	// Indexed is the universe of hunk IDs in the live diff, excluding ignored
	// files. This is the set the story is expected to cover.
	Indexed []HunkID

	// Ignored is the hunk IDs from ignored files (ignore_patterns). They are
	// pre-placed into support[] with reason "ignored" and counted as placed;
	// the agent never sees them.
	Ignored []HunkID

	// LiveFingerprint is the scope fingerprint recomputed from the live diff at
	// ingest time. It is compared against Story.ScopeFingerprint (snapshotted at
	// --prep time) to detect working-tree drift.
	LiveFingerprint string
}

// Result reports the outcome of an ingest attempt. Coverage is always
// populated (printed to stdout as JSON on every ingest, success or failure).
// Saved is true only when the caller should persist Ingest.Story.
type Result struct {
	Saved    bool
	Coverage *session.StoryCoverage
}

// Sentinel errors so callers can distinguish rejection reasons if needed.
var (
	ErrDrift      = errors.New("diff changed since prep — re-run `crit story --prep` and re-author")
	ErrDuplicate  = errors.New("story places the same hunk in more than one chapter/support entry")
	ErrBelowFloor = errors.New("story places fewer than half of the diff's hunks")
)

// Fingerprint computes a stable sha256 over the sorted hunk IDs. It is used
// both at prep time (snapshotted into Story.ScopeFingerprint) and at ingest
// time (recomputed from the live diff) to detect that the working tree moved
// while the agent was authoring.
func Fingerprint(hunks []HunkID) string {
	ids := make([]string, len(hunks))
	for i, h := range hunks {
		ids[i] = h.String()
	}
	sort.Strings(ids)
	sum := sha256.New()
	for _, id := range ids {
		sum.Write([]byte(id))
		sum.Write([]byte{0})
	}
	return fmt.Sprintf("%x", sum.Sum(nil))
}

// Run validates and (per §8 policy) repairs a story against the live diff.
// Order is strict: drift check first, then duplicates, then the placed floor.
// It mutates in.Story in place when the outcome is a save. The returned Result
// always carries a Coverage report.
func Run(in Ingest) (Result, error) {
	coverage := &session.StoryCoverage{Indexed: len(in.Indexed)}

	// 1. Drift check first. A mismatch means the working tree moved since prep.
	if in.Story.ScopeFingerprint != "" && in.LiveFingerprint != "" &&
		in.Story.ScopeFingerprint != in.LiveFingerprint {
		return Result{Coverage: coverage}, ErrDrift
	}

	indexedSet := make(map[HunkID]struct{}, len(in.Indexed))
	for _, h := range in.Indexed {
		indexedSet[h] = struct{}{}
	}

	// Pre-place ignored files into support[] with reason "ignored" BEFORE the
	// coverage math. They count as placed and are never treated as missing.
	prePlaceIgnored(in.Story, in.Ignored)

	// 2. Tally placed hunks and detect duplicates (a hunk referenced by >=2
	// chapters, or by a chapter and a support entry).
	placedSet, duplicated := tallyPlacement(in.Story, indexedSet)
	coverage.Placed = len(placedSet)

	// 3. Duplicates -> strict reject.
	if len(duplicated) > 0 {
		coverage.Duplicated = idStrings(duplicated)
		return Result{Coverage: coverage}, ErrDuplicate
	}

	// Compute missing = indexed - placed.
	var missing []HunkID
	for _, h := range in.Indexed {
		if _, ok := placedSet[h]; !ok {
			missing = append(missing, h)
		}
	}

	// 4. Missing -> repair with a floor.
	if len(missing) > 0 {
		if len(in.Indexed) == 0 || float64(coverage.Placed)/float64(len(in.Indexed)) < placedFloor {
			coverage.Missing = idStrings(missing)
			return Result{Coverage: coverage}, ErrBelowFloor
		}
		backfillSupport(in.Story, missing, ReasonAutoRepaired)
		coverage.Missing = idStrings(missing)
		coverage.AutoRepaired = true
	}

	// 5. ok is true only when the story saved with zero repairs.
	coverage.OK = !coverage.AutoRepaired
	in.Story.Coverage = coverage
	return Result{Saved: true, Coverage: coverage}, nil
}

// prePlaceIgnored appends the ignored-file hunks to support[] under the
// "ignored" sentinel, before coverage math runs.
func prePlaceIgnored(st *session.Story, ignored []HunkID) {
	if len(ignored) == 0 {
		return
	}
	backfillSupport(st, ignored, ReasonIgnored)
}

// backfillSupport appends hunks to support[] as a single entry with reason.
func backfillSupport(st *session.Story, hunks []HunkID, reason string) {
	refs := make([]session.StoryHunkRef, len(hunks))
	for i, h := range hunks {
		refs[i] = idRef(h)
	}
	st.Support = append(st.Support, session.StorySupportEntry{HunkRefs: refs, Reason: reason})
}

// tallyPlacement counts how many times each indexed hunk is referenced across
// chapters and (non-ignored) support entries. It returns the set of placed
// hunks and the hunks referenced more than once (duplicates). Refs to
// ignored/unknown hunks are tolerated but don't count toward placement.
func tallyPlacement(st *session.Story, indexedSet map[HunkID]struct{}) (map[HunkID]struct{}, []HunkID) {
	seen := make(map[HunkID]int)
	count := func(r session.StoryHunkRef) {
		id := refID(r)
		if _, ok := indexedSet[id]; ok {
			seen[id]++
		}
	}
	for _, ch := range st.Chapters {
		for _, r := range ch.HunkRefs {
			count(r)
		}
	}
	for _, sup := range st.Support {
		if sup.Reason == ReasonIgnored {
			continue // pre-placed; not authored coverage
		}
		for _, r := range sup.HunkRefs {
			count(r)
		}
	}

	placedSet := make(map[HunkID]struct{}, len(seen))
	var duplicated []HunkID
	for id, n := range seen {
		placedSet[id] = struct{}{}
		if n > 1 {
			duplicated = append(duplicated, id)
		}
	}
	return placedSet, duplicated
}

// idStrings renders hunk IDs sorted for stable coverage reports.
func idStrings(ids []HunkID) []string {
	out := make([]string, len(ids))
	for i, h := range ids {
		out[i] = h.String()
	}
	sort.Strings(out)
	return out
}
