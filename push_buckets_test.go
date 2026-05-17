package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBucketComments_PureLayer(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", StartLine: 10, EndLine: 10, Body: "first", DiffScope: "layer", HeadSHA: "head1"},
				{ID: "c2", StartLine: 20, EndLine: 22, Body: "second", DiffScope: "layer", HeadSHA: "head1"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "head1", true)
	if len(b.Postable) != 2 {
		t.Errorf("Postable=%d, want 2", len(b.Postable))
	}
	if len(b.FullStack) != 0 || len(b.Unmapped) != 0 {
		t.Errorf("expected only Postable; got fullstack=%d unmapped=%d", len(b.FullStack), len(b.Unmapped))
	}
}

func TestBucketComments_MixedScope(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "layer", DiffScope: "layer", HeadSHA: "h1"},
				{ID: "c2", Body: "fs", DiffScope: "full_stack", HeadSHA: "h1"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "h1", true)
	if len(b.Postable) != 1 || b.Postable[0].Comment.Body != "layer" {
		t.Errorf("Postable=%+v, want [layer]", b.Postable)
	}
	if len(b.FullStack) != 1 || b.FullStack[0].Comment.Body != "fs" {
		t.Errorf("FullStack=%+v, want [fs]", b.FullStack)
	}
}

func TestBucketComments_StaleHead(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "outdated", DiffScope: "layer", HeadSHA: "abc12345aaa"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "newhead", true)
	if len(b.Unmapped) != 1 {
		t.Fatalf("Unmapped=%d, want 1", len(b.Unmapped))
	}
	got := b.Unmapped[0]
	if got.Reason != bucketReasonStale {
		t.Errorf("Reason=%q, want stale", got.Reason)
	}
	if !strings.Contains(got.Detail, "abc1234") {
		t.Errorf("Detail %q should mention old SHA prefix", got.Detail)
	}
}

func TestBucketComments_NoAnchor(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "no anchor", DiffScope: "layer", HeadSHA: ""},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "head1", true)
	if len(b.Unmapped) != 1 {
		t.Fatalf("Unmapped=%d, want 1", len(b.Unmapped))
	}
	if b.Unmapped[0].Reason != bucketReasonNoAnchor {
		t.Errorf("Reason=%q, want no-anchor", b.Unmapped[0].Reason)
	}
}

func TestBucketComments_WorkingTree(t *testing.T) {
	// Empty HeadSHA when NOT in range mode is the today's-default: legacy
	// comment, postable.
	cj := CritJSON{
		ActiveDiffScope: "",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "legacy", DiffScope: "", HeadSHA: ""},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.Postable) != 1 {
		t.Errorf("Postable=%d, want 1", len(b.Postable))
	}
	if len(b.FullStack) != 0 || len(b.Unmapped) != 0 {
		t.Errorf("non-postable buckets should be empty: fs=%d unmapped=%d", len(b.FullStack), len(b.Unmapped))
	}
}

func TestBucketComments_ResolvedSkipped(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "done", DiffScope: "layer", HeadSHA: "h1", Resolved: true},
				{ID: "c2", Body: "still-fs", DiffScope: "full_stack", HeadSHA: "h1", Resolved: true},
				{ID: "c3", Body: "stale-resolved", DiffScope: "layer", HeadSHA: "stale", Resolved: true},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "h1", true)
	if len(b.Postable)+len(b.FullStack)+len(b.Unmapped) != 0 {
		t.Errorf("resolved comments should be excluded everywhere, got %+v", b)
	}
}

func TestBucketComments_AlreadyOnGitHub(t *testing.T) {
	// Comments with GitHubID != 0 are already pushed; mirror the existing
	// critJSONToGHComments behavior and skip them.
	cj := CritJSON{
		ActiveDiffScope: "",
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", Body: "legacy", DiffScope: "", GitHubID: 999},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.Postable)+len(b.FullStack)+len(b.Unmapped) != 0 {
		t.Errorf("expected GitHub-anchored comment to be skipped, got %+v", b)
	}
}

func TestRenderOrphanMarkdown_StructureAndContent(t *testing.T) {
	b := pushBuckets{
		FullStack: []scopedComment{{
			Path:    "src/foo.go",
			Comment: Comment{StartLine: 12, EndLine: 14, Body: "use the helper here"},
			Reason:  bucketReasonFullStack,
		}},
		Unmapped: []scopedComment{{
			Path:    "src/bar.go",
			Comment: Comment{StartLine: 5, EndLine: 5, Body: "stale review"},
			Reason:  bucketReasonStale,
			Detail:  "was abc1234",
		}},
	}
	out := renderOrphanMarkdown(295, b)

	wantSubstrings := []string{
		"# Comments not pushed to PR #295",
		"## Full-stack-only (1)",
		"### src/foo.go:12-14",
		"use the helper here",
		"## Stale or unanchored (1)",
		"### src/bar.go:5",
		"_was abc1234_",
		"stale review",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}

func TestRenderOrphanMarkdown_OmitsEmptySections(t *testing.T) {
	b := pushBuckets{FullStack: []scopedComment{{
		Path:    "x.go",
		Comment: Comment{EndLine: 1, Body: "x"},
		Reason:  bucketReasonFullStack,
	}}}
	out := renderOrphanMarkdown(1, b)
	if strings.Contains(out, "Stale or unanchored") {
		t.Errorf("empty section should be omitted, got:\n%s", out)
	}
}

func TestWriteOrphanExport_CreatesFileAtExpectedPath(t *testing.T) {
	dir := t.TempDir()
	b := pushBuckets{Unmapped: []scopedComment{{
		Path:    "a.go",
		Comment: Comment{EndLine: 1, Body: "x"},
		Reason:  bucketReasonNoAnchor,
	}}}

	path, err := writeOrphanExport(42, b, dir)
	if err != nil {
		t.Fatalf("writeOrphanExport: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("export landed in %s, want under %s", filepath.Dir(path), dir)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "42-") || !strings.HasSuffix(base, ".md") {
		t.Errorf("filename %q should match `<pr>-<ts>.md`", base)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "Comments not pushed to PR #42") {
		t.Errorf("file body missing header, got:\n%s", string(data))
	}
}

func TestWriteOrphanExport_CreatesDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "nested", "exports")
	b := pushBuckets{Unmapped: []scopedComment{{
		Path:    "a.go",
		Comment: Comment{EndLine: 1, Body: "x"},
		Reason:  bucketReasonNoAnchor,
	}}}
	if _, err := writeOrphanExport(1, b, target); err != nil {
		t.Fatalf("writeOrphanExport: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected dir at %s, err=%v", target, err)
	}
}

func TestSummarizeBuckets_FormatsCorrectly(t *testing.T) {
	b := pushBuckets{
		Postable:  make([]scopedComment, 12),
		FullStack: make([]scopedComment, 2),
		Unmapped:  make([]scopedComment, 1),
	}
	got := summarizeBuckets(295, b)
	want := "Push plan for PR #295: 12 postable, 2 full-stack, 1 stale."
	if got != want {
		t.Errorf("summarizeBuckets:\n got: %q\nwant: %q", got, want)
	}
}

func TestDetailedDryRun_PerBucketSections(t *testing.T) {
	b := pushBuckets{
		Postable: []scopedComment{{
			Path: "a.go", Comment: Comment{EndLine: 5, Body: "ship it"},
		}},
		FullStack: []scopedComment{{
			Path: "b.go", Comment: Comment{EndLine: 8, Body: "fs body"}, Reason: bucketReasonFullStack,
		}},
		Unmapped: []scopedComment{{
			Path: "c.go", Comment: Comment{EndLine: 12, Body: "stale body"}, Reason: bucketReasonStale,
		}},
	}
	out := detailedDryRun(b)

	want := []string{
		"Postable (1):",
		"a.go:5: ship it",
		"Full-stack-only (1):",
		"b.go:8: fs body",
		"Stale or unanchored (1):",
		"c.go:12: stale body",
	}
	for _, s := range want {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}

func TestDetailedDryRun_TruncatesLongBody(t *testing.T) {
	long := strings.Repeat("x", 200)
	b := pushBuckets{Postable: []scopedComment{{
		Path: "a.go", Comment: Comment{EndLine: 1, Body: long},
	}}}
	out := detailedDryRun(b)
	if strings.Contains(out, strings.Repeat("x", 100)) {
		t.Errorf("body should be truncated, got line containing 100+ x")
	}
}

func TestBucketsToGHComments_ShapesCorrectly(t *testing.T) {
	postable := []scopedComment{
		{Path: "a.go", Comment: Comment{StartLine: 3, EndLine: 3, Body: "single"}},
		{Path: "b.go", Comment: Comment{StartLine: 5, EndLine: 8, Body: "range"}},
	}
	got := bucketsToGHComments(postable, nil)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	// Single-line: no start_line.
	if _, ok := got[0]["start_line"]; ok {
		t.Errorf("single-line comment should not carry start_line: %+v", got[0])
	}
	if got[0]["line"] != 3 || got[0]["body"] != "single" {
		t.Errorf("first comment shape mismatch: %+v", got[0])
	}
	// Multi-line: start_line + start_side.
	if got[1]["start_line"] != 5 || got[1]["start_side"] != "RIGHT" {
		t.Errorf("multi-line comment missing start_line/start_side: %+v", got[1])
	}
}

func TestBucketsToGHComments_EmptyInput(t *testing.T) {
	if got := bucketsToGHComments(nil, nil); got != nil {
		t.Errorf("nil input should return nil, got %+v", got)
	}
	if got := bucketsToGHComments([]scopedComment{}, nil); got != nil {
		t.Errorf("empty input should return nil, got %+v", got)
	}
}

// Default (nil) rewriter strips local attachment refs and appends a
// "view in Crit" placeholder. Verifies the body sent to GitHub doesn't
// leak relative attachments/<uuid> paths.
func TestBucketsToGHComments_StripsLocalAttachmentRefsByDefault(t *testing.T) {
	uuid, _ := randomUUID()
	body := "look at this:\n\n![](attachments/" + uuid + ".png)\n\nthoughts?"
	postable := []scopedComment{{
		Path: "ui.tsx", Comment: Comment{StartLine: 1, EndLine: 1, Body: body},
	}}
	got := bucketsToGHComments(postable, nil)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	out, _ := got[0]["body"].(string)
	if strings.Contains(out, "](attachments/") {
		t.Errorf("body still contains local attachment ref: %q", out)
	}
	if !strings.Contains(out, "view in Crit") {
		t.Errorf("expected placeholder note in body: %q", out)
	}
}

// Generic delegation check: when the caller passes a rewriter that swaps
// attachments/<uuid> for some other URL, bucketsToGHComments forwards the
// body unchanged through that rewriter (no placeholder appended). Not a
// production scenario — production passes stripBodyRewriter — but the
// rewriter parameter is part of the function's contract.
func TestBucketsToGHComments_SwapsLocalAttachmentRefsWhenUpload(t *testing.T) {
	uuid, _ := randomUUID()
	body := "look at this:\n\n![bug.png](attachments/" + uuid + ".png)\n\nthoughts?"
	swap := func(b string) string {
		return strings.ReplaceAll(
			b,
			"attachments/"+uuid+".png",
			"https://raw.githubusercontent.com/o/r/feature/.crit/images/"+uuid+".png",
		)
	}
	postable := []scopedComment{{
		Path: "ui.tsx", Comment: Comment{StartLine: 1, EndLine: 1, Body: body},
	}}
	got := bucketsToGHComments(postable, swap)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	out, _ := got[0]["body"].(string)
	if !strings.Contains(out, "raw.githubusercontent.com/o/r/feature/.crit/images/"+uuid+".png") {
		t.Errorf("body missing GitHub raw URL: %q", out)
	}
	if strings.Contains(out, "](attachments/") {
		t.Errorf("body still references local attachments/: %q", out)
	}
	if strings.Contains(out, "view in Crit") {
		t.Errorf("body should not carry strip placeholder when upload swaps URL: %q", out)
	}
}

func TestBucketComments_ReviewLevelSurfaced(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "",
		ReviewComments: []Comment{
			{ID: "r1", Body: "general feedback"},
			{ID: "r2", Body: "another", Resolved: true},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.ReviewLevel) != 1 {
		t.Fatalf("ReviewLevel=%d want 1 (resolved must be excluded)", len(b.ReviewLevel))
	}
	if b.ReviewLevel[0].Comment.ID != "r1" {
		t.Errorf("got %q want r1", b.ReviewLevel[0].Comment.ID)
	}
	if b.ReviewLevel[0].Path != "" {
		t.Errorf("review-level entries should carry empty Path; got %q", b.ReviewLevel[0].Path)
	}
}

func TestSummarizeBuckets_AppendsReviewLevelOnlyWhenPresent(t *testing.T) {
	withRL := pushBuckets{
		Postable:    make([]scopedComment, 1),
		ReviewLevel: make([]scopedComment, 3),
	}
	want := "Push plan for PR #1: 1 postable, 0 full-stack, 0 stale, 3 review-level."
	if got := summarizeBuckets(1, withRL); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	noRL := pushBuckets{Postable: make([]scopedComment, 1)}
	want = "Push plan for PR #1: 1 postable, 0 full-stack, 0 stale."
	if got := summarizeBuckets(1, noRL); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDetailedDryRun_ReviewLevelSection(t *testing.T) {
	b := pushBuckets{
		ReviewLevel: []scopedComment{{
			Comment: Comment{ID: "r1", Body: "ship it but..."},
			Detail:  "review-level (not pushable)",
		}},
	}
	out := detailedDryRun(b)
	if !strings.Contains(out, "Review-level (not pushable) (1):") {
		t.Errorf("missing review-level section in:\n%s", out)
	}
	if !strings.Contains(out, "ship it but...") {
		t.Errorf("body missing in:\n%s", out)
	}
}

func TestRenderOrphanMarkdown_IncludesReviewLevel(t *testing.T) {
	b := pushBuckets{
		ReviewLevel: []scopedComment{{
			Comment: Comment{Body: "general note"},
			Detail:  "review-level (not pushable)",
		}},
	}
	out := renderOrphanMarkdown(42, b)
	if !strings.Contains(out, "## Review-level (not pushable) (1)") {
		t.Errorf("missing section in:\n%s", out)
	}
	if !strings.Contains(out, "general note") {
		t.Errorf("body missing in:\n%s", out)
	}
}

// TestBucketComments_DeterministicOrder verifies that bucket contents are
// stable (sorted by path) so dry-run output and export files diff cleanly
// across runs.
func TestBucketComments_DeterministicOrder(t *testing.T) {
	cj := CritJSON{
		ActiveDiffScope: "layer",
		Files: map[string]CritJSONFile{
			"z.go": {Comments: []Comment{{ID: "z", DiffScope: "layer", HeadSHA: "h", Body: "z"}}},
			"a.go": {Comments: []Comment{{ID: "a", DiffScope: "layer", HeadSHA: "h", Body: "a"}}},
			"m.go": {Comments: []Comment{{ID: "m", DiffScope: "layer", HeadSHA: "h", Body: "m"}}},
		},
	}
	for i := 0; i < 5; i++ {
		b := bucketCommentsForPush(cj, "h", true)
		if len(b.Postable) != 3 {
			t.Fatalf("len=%d", len(b.Postable))
		}
		paths := []string{b.Postable[0].Path, b.Postable[1].Path, b.Postable[2].Path}
		want := []string{"a.go", "m.go", "z.go"}
		for j := range want {
			if paths[j] != want[j] {
				t.Errorf("iter %d order wrong: got %v want %v", i, paths, want)
			}
		}
	}
}

// TestPushBlockedByFullStackScope asserts the predicate that gates `crit push`
// when comments were authored under the cumulative stack range (full_stack
// scope). Only the literal "full_stack" disk scope triggers the gate; layer
// scope and unset (working tree) do not. The gate message wording is locked
// down by test/test-diff.sh Instance 6, so verify it here too.
func TestPushBlockedByFullStackScope(t *testing.T) {
	tests := []struct {
		scope string
		want  bool
	}{
		{"", false},
		{"layer", false},
		{"full_stack", true},
		{"full-stack", false}, // ActiveDiffScope is normalized to underscore form on disk.
	}
	for _, tc := range tests {
		if got := pushBlockedByFullStackScope(tc.scope); got != tc.want {
			t.Errorf("pushBlockedByFullStackScope(%q)=%v want %v", tc.scope, got, tc.want)
		}
	}
	if fullStackPushGateMessage != "Switch to Layer diff before posting a platform review" {
		t.Errorf("gate message changed: %q — test/test-diff.sh Instance 6 will fail", fullStackPushGateMessage)
	}
}

func TestBucketComments_DOMAnchorFiltered(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"/dashboard": {Comments: []Comment{
				{ID: "pin1", Body: "pin", DOMAnchor: &DOMAnchor{Pathname: "/dashboard", CSSSelector: "#h1"}},
				{ID: "code1", StartLine: 10, EndLine: 10, Body: "code"},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.Postable) != 1 || b.Postable[0].Comment.ID != "code1" {
		t.Errorf("Postable = %v; live pin must be filtered", b.Postable)
	}
	for _, sc := range append(b.FullStack, b.Unmapped...) {
		if sc.Comment.DOMAnchor != nil {
			t.Errorf("live pin leaked into FullStack/Unmapped: %v", sc)
		}
	}
}
