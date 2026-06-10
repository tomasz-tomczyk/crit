package github

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportsDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	got, err := exportsDir()
	if err != nil {
		t.Fatalf("exportsDir() error: %v", err)
	}
	want := filepath.Join(home, ".crit", "exports")
	if got != want {
		t.Errorf("exportsDir() = %q, want %q", got, want)
	}
}

// sampleBuckets returns a pushBuckets covering every non-empty section so the
// rendering/summary functions exercise all their branches.
func sampleBuckets() PushBuckets {
	return PushBuckets{
		Postable: []scopedComment{
			{Path: "a.go", Comment: Comment{StartLine: 10, EndLine: 10, Body: "postable body"}},
		},
		FullStack: []scopedComment{
			{Path: "b.go", Comment: Comment{StartLine: 5, EndLine: 8, Body: "full stack"}, Detail: "full-stack scope"},
		},
		Unmapped: []scopedComment{
			{Path: "c.go", Comment: Comment{StartLine: 0, EndLine: 3, Body: "unmapped"}, Detail: "stale head"},
		},
		ReviewLevel: []scopedComment{
			{Comment: Comment{Body: "review-level note"}},
		},
	}
}

// The exported export.go wrappers are thin shims over the lowercase functions
// (which push_buckets_test.go covers directly). These tests exercise the
// exported surface used by callers outside the package — closing the 0%
// coverage on export.go without re-testing the underlying behavior in depth.
func TestExportedPushWrappers(t *testing.T) {
	b := sampleBuckets()

	if got := SummarizeBuckets(295, b); !strings.Contains(got, "PR #295") {
		t.Errorf("SummarizeBuckets = %q", got)
	}
	if got := DetailedDryRun(b); !strings.Contains(got, "Postable (1)") {
		t.Errorf("DetailedDryRun = %q", got)
	}
	if got := RenderOrphanMarkdown(295, b); !strings.Contains(got, "PR #295") {
		t.Errorf("RenderOrphanMarkdown = %q", got)
	}

	comments := BucketsToGHComments(b.Postable, nil)
	if len(comments) != 1 || comments[0]["path"] != "a.go" {
		t.Errorf("BucketsToGHComments = %+v", comments)
	}
}

func TestWriteOrphanExport_Exported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "exports")
	path, err := WriteOrphanExport(42, sampleBuckets(), dir)
	if err != nil {
		t.Fatalf("WriteOrphanExport error: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("export written to %q, want under %q", path, dir)
	}
	if !strings.HasPrefix(filepath.Base(path), "42-") || !strings.HasSuffix(path, ".md") {
		t.Errorf("unexpected export filename: %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading export: %v", err)
	}
	if !strings.Contains(string(data), "# Comments not pushed to PR #42") {
		t.Errorf("export file missing header:\n%s", data)
	}
	// Postable comments are not part of the orphan export.
	if strings.Contains(string(data), "postable body") {
		t.Errorf("orphan export should not include postable comments:\n%s", data)
	}
}

func TestRunPushDryRun(t *testing.T) {
	out := captureStdout(t, func() {
		runPushDryRun(pushContext{prNumber: 7}, sampleBuckets())
	})
	for _, want := range []string{"Push plan for PR #7", "Postable (1)", "crit push --pr 7"} {
		if !strings.Contains(out, want) {
			t.Errorf("runPushDryRun output missing %q:\n%s", want, out)
		}
	}
}
