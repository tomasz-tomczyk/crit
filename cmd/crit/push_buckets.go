package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// bucketReason describes why a comment was diverted from the postable bucket.
// Empty bucketReason means the comment is postable.
type bucketReason string

const (
	bucketReasonFullStack bucketReason = "full-stack scope"
	bucketReasonNoAnchor  bucketReason = "no anchor"
	bucketReasonStale     bucketReason = "stale head"
)

// scopedComment pairs a Comment with its file path and (when bucketed off the
// happy path) the reason it was set aside. Detail carries human-readable
// context — for stale comments this includes the SHA the comment was authored
// against, so the export file is self-explanatory.
type scopedComment struct {
	Path    string
	Comment Comment
	Reason  bucketReason
	Detail  string
}

// pushBuckets is the result of partitioning unresolved comments into the
// outcomes a `crit push` can produce: comments that go to GitHub (Postable),
// comments that stay local because they're full-stack scope (FullStack),
// comments that stay local because their PR-side anchor is missing or stale
// (Unmapped), and review-level comments which the GitHub inline-comment API
// has no representation for (ReviewLevel).
type pushBuckets struct {
	Postable    []scopedComment
	FullStack   []scopedComment
	Unmapped    []scopedComment
	ReviewLevel []scopedComment // review-level (general) comments — not pushable to PR review API
}

// bucketCommentsForPush partitions cj's unresolved file comments by their
// push eligibility.
//
// inRangeMode is true when the active focus is a Range (cj.ActiveDiffScope
// non-empty). currentHeadSHA is the PR's current head SHA when known; pass
// "" to skip the stale-head check (e.g. when gh is unreachable in dry-run).
//
// Rules, evaluated per comment in order:
//
//  1. Resolved comments are excluded entirely.
//  2. DiffScope == "full_stack"  -> FullStack
//  3. inRangeMode && HeadSHA == ""                       -> Unmapped (no anchor)
//  4. inRangeMode && currentHeadSHA != "" &&
//     HeadSHA != currentHeadSHA                          -> Unmapped (stale)
//  5. !inRangeMode && DiffScope != ""                    -> FullStack
//     (working-tree push only includes legacy comments;
//     a full_stack DiffScope is impossible here because
//     rule 2 caught it, leaving only "layer" — but layer
//     authored in range mode shouldn't be posted from
//     working-tree mode either, so it falls to FullStack)
//  6. Otherwise                                          -> Postable
//
// Output is sorted by Path then EndLine for deterministic display.
//
// cj.ReviewComments are surfaced in the ReviewLevel bucket (path == ""), so
// dry-run output explicitly lists them as "not pushable" rather than silently
// dropping them. The GitHub inline-comment API has no representation for
// review-level comments — they're displayed locally only.
func bucketCommentsForPush(cj CritJSON, currentHeadSHA string, inRangeMode bool) pushBuckets {
	var buckets pushBuckets

	paths := make([]string, 0, len(cj.Files))
	for path := range cj.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fe := cj.Files[path]
		for _, c := range fe.Comments {
			if c.Resolved {
				continue
			}
			if c.GitHubID != 0 {
				continue // already on GitHub; skip (matches critJSONToGHComments)
			}
			if c.DOMAnchor != nil {
				continue // live pins are local-only; never post to GitHub
			}
			sc := scopedComment{Path: path, Comment: c}
			classifyComment(&sc, currentHeadSHA, inRangeMode)
			switch sc.Reason {
			case "":
				buckets.Postable = append(buckets.Postable, sc)
			case bucketReasonFullStack:
				buckets.FullStack = append(buckets.FullStack, sc)
			case bucketReasonNoAnchor, bucketReasonStale:
				buckets.Unmapped = append(buckets.Unmapped, sc)
			}
		}
	}

	for _, rc := range cj.ReviewComments {
		if rc.Resolved {
			continue
		}
		buckets.ReviewLevel = append(buckets.ReviewLevel, scopedComment{
			Comment: rc,
			Detail:  "review-level (not pushable)",
		})
	}
	return buckets
}

// classifyComment fills sc.Reason and sc.Detail based on the bucketing rules.
// Split out from bucketCommentsForPush to keep that function's loop body small
// and the cyclomatic complexity bounded.
func classifyComment(sc *scopedComment, currentHeadSHA string, inRangeMode bool) {
	c := sc.Comment
	if c.DiffScope == string(DiffScopeFullStack) {
		sc.Reason = bucketReasonFullStack
		return
	}
	if !inRangeMode {
		// Working-tree push: only legacy (no DiffScope) comments are postable.
		// Layer-scope comments from a previous range session are bucketed as
		// full-stack (they're for a different focus, can't be posted here).
		if c.DiffScope != "" {
			sc.Reason = bucketReasonFullStack
			sc.Detail = "layer-scope comment in working-tree push"
		}
		return
	}
	// Range mode.
	if c.HeadSHA == "" {
		sc.Reason = bucketReasonNoAnchor
		return
	}
	if currentHeadSHA != "" && c.HeadSHA != currentHeadSHA {
		sc.Reason = bucketReasonStale
		sc.Detail = "was " + shortSHA(c.HeadSHA)
		return
	}
}

// renderOrphanMarkdown formats the Full-stack-only and Stale buckets as a
// human-readable markdown document. Empty sections are omitted.
func renderOrphanMarkdown(prNum int, b pushBuckets) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Comments not pushed to PR #%d\n\n", prNum)
	fmt.Fprintf(&sb, "_Generated by crit at %s._\n\n", time.Now().UTC().Format(time.RFC3339))

	if len(b.FullStack) > 0 {
		fmt.Fprintf(&sb, "## Full-stack-only (%d)\n\n", len(b.FullStack))
		for _, sc := range b.FullStack {
			writeOrphanComment(&sb, sc)
		}
	}
	if len(b.Unmapped) > 0 {
		fmt.Fprintf(&sb, "## Stale or unanchored (%d)\n\n", len(b.Unmapped))
		for _, sc := range b.Unmapped {
			writeOrphanComment(&sb, sc)
		}
	}
	if len(b.ReviewLevel) > 0 {
		fmt.Fprintf(&sb, "## Review-level (not pushable) (%d)\n\n", len(b.ReviewLevel))
		for _, sc := range b.ReviewLevel {
			writeOrphanComment(&sb, sc)
		}
	}
	return sb.String()
}

// writeOrphanComment renders a single comment block in the export markdown.
// Format: "### path:start-end" + optional reason detail + body.
func writeOrphanComment(sb *strings.Builder, sc scopedComment) {
	fmt.Fprintf(sb, "### %s\n\n", commentLocator(sc))
	if sc.Detail != "" {
		fmt.Fprintf(sb, "_%s_\n\n", sc.Detail)
	}
	fmt.Fprintf(sb, "%s\n\n", strings.TrimRight(sc.Comment.Body, "\n"))
}

// commentLocator formats "path:line" or "path:start-end" for display.
// Comments without a path (review-level — not currently bucketed but
// future-proofed) render as "(review)".
func commentLocator(sc scopedComment) string {
	if sc.Path == "" {
		return "(review)"
	}
	if sc.Comment.StartLine == sc.Comment.EndLine || sc.Comment.StartLine == 0 {
		return fmt.Sprintf("%s:%d", sc.Path, sc.Comment.EndLine)
	}
	return fmt.Sprintf("%s:%d-%d", sc.Path, sc.Comment.StartLine, sc.Comment.EndLine)
}

// writeOrphanExport writes the markdown for the FullStack + Unmapped buckets
// to a timestamped file under exportDir. Returns the path it wrote to.
//
// Filename: <prNum>-<UTC ISO8601>.md, e.g. 295-20260428T143015Z.md.
// exportDir is created with 0o755 if missing.
func writeOrphanExport(prNum int, b pushBuckets, exportDir string) (string, error) {
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return "", fmt.Errorf("creating export dir: %w", err)
	}
	name := fmt.Sprintf("%d-%s.md", prNum, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(exportDir, name)
	body := renderOrphanMarkdown(prNum, b)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("writing export: %w", err)
	}
	return path, nil
}

// summarizeBuckets returns a one-line, human-readable summary suitable for
// the dry-run header. e.g.
// "Push plan for PR #295: 12 postable, 2 full-stack, 1 stale, 3 review-level."
// The review-level segment is omitted when there are none, to avoid noise on
// the common path where a review only contains inline comments.
func summarizeBuckets(prNum int, b pushBuckets) string {
	base := fmt.Sprintf("Push plan for PR #%d: %d postable, %d full-stack, %d stale",
		prNum, len(b.Postable), len(b.FullStack), len(b.Unmapped))
	if len(b.ReviewLevel) > 0 {
		return fmt.Sprintf("%s, %d review-level.", base, len(b.ReviewLevel))
	}
	return base + "."
}

// detailedDryRun returns a multi-line dump grouped by bucket, listing each
// comment as "path:line: <first 60 chars of body>". Empty buckets produce no
// section so a clean push has no noise.
func detailedDryRun(b pushBuckets) string {
	var sb strings.Builder
	if len(b.Postable) > 0 {
		fmt.Fprintf(&sb, "Postable (%d):\n", len(b.Postable))
		for _, sc := range b.Postable {
			writeDryRunLine(&sb, sc)
		}
		sb.WriteString("\n")
	}
	if len(b.FullStack) > 0 {
		fmt.Fprintf(&sb, "Full-stack-only (%d):\n", len(b.FullStack))
		for _, sc := range b.FullStack {
			writeDryRunLine(&sb, sc)
		}
		sb.WriteString("\n")
	}
	if len(b.Unmapped) > 0 {
		fmt.Fprintf(&sb, "Stale or unanchored (%d):\n", len(b.Unmapped))
		for _, sc := range b.Unmapped {
			writeDryRunLine(&sb, sc)
		}
		sb.WriteString("\n")
	}
	if len(b.ReviewLevel) > 0 {
		fmt.Fprintf(&sb, "Review-level (not pushable) (%d):\n", len(b.ReviewLevel))
		for _, sc := range b.ReviewLevel {
			writeDryRunLine(&sb, sc)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// writeDryRunLine appends one comment line to the dry-run output. Body is
// truncated to 60 runes and newlines are squashed so the line stays compact.
func writeDryRunLine(sb *strings.Builder, sc scopedComment) {
	// Dry-run preview shows the strip view (attachment refs gone, placeholder
	// note appended). The live push may upload-and-swap instead — we don't
	// run that side-effect here because dry-run is read-only.
	stripped, _ := stripAttachmentReferences(sc.Comment.Body)
	body := strings.ReplaceAll(stripped, "\n", " ")
	fmt.Fprintf(sb, "  %s: %s\n", commentLocator(sc), truncateStr(body, 60))
}

// bucketsToGHComments converts the postable bucket into the slice shape
// createGHReview expects. Mirrors critJSONToGHComments (github.go) but only
// acts on already-filtered postable comments — the resolved/GitHubID checks
// are redundant here (bucketCommentsForPush enforces them) but cheap and
// defensive.
//
// rewrite is applied to each comment body before it ships. Production push
// passes stripBodyRewriter; tests may pass nil for strip behavior.
func bucketsToGHComments(postable []scopedComment, rewrite bodyRewriter) []map[string]any {
	if len(postable) == 0 {
		return nil
	}
	if rewrite == nil {
		rewrite = stripBodyRewriter
	}
	out := make([]map[string]any, 0, len(postable))
	for _, sc := range postable {
		c := sc.Comment
		if c.Resolved || c.GitHubID != 0 {
			continue
		}
		comment := map[string]any{
			"path": sc.Path,
			"line": c.EndLine,
			"side": "RIGHT",
			"body": rewrite(c.Body),
		}
		if c.StartLine != c.EndLine && c.StartLine > 0 {
			comment["start_line"] = c.StartLine
			comment["start_side"] = "RIGHT"
		}
		out = append(out, comment)
	}
	return out
}

// exportsDir returns the path to ~/.crit/exports/ — the directory used for
// orphan markdown dumps. Mirrors the pattern of reviewsDir / sessionsDir.
func exportsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "exports"), nil
}
