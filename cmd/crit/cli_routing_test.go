package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoutePositionalArgs(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlFile, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		want positionalRoute
	}{
		{"pr url", []string{"https://github.com/a/b/pull/295"}, positionalRoutePRReview},
		{"pr url files tab", []string{"https://github.com/a/b/pull/295/files"}, positionalRoutePRReview},
		{"live localhost", []string{"http://localhost:3000"}, positionalRouteLive},
		{"live https", []string{"https://example.com/app"}, positionalRouteLive},
		{"preview html", []string{htmlFile}, positionalRoutePreview},
		{"plain file", []string{"README.md"}, positionalRouteReview},
		{"url plus file", []string{"http://a.com", "README.md"}, positionalRouteReview},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := routePositionalArgs(c.args); got != c.want {
				t.Errorf("routePositionalArgs(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestPRReviewArgs(t *testing.T) {
	args, ok := prReviewArgs([]string{"https://github.com/a/b/pull/42"})
	if !ok {
		t.Fatal("expected ok=true for PR URL")
	}
	want := []string{"--pr", "https://github.com/a/b/pull/42"}
	if len(args) != len(want) || args[0] != want[0] || args[1] != want[1] {
		t.Errorf("got %v, want %v", args, want)
	}

	if _, ok := prReviewArgs([]string{"https://example.com"}); ok {
		t.Error("expected ok=false for non-PR URL")
	}
	if _, ok := prReviewArgs([]string{"295"}); ok {
		t.Error("expected ok=false for bare PR number")
	}
}

func TestRunPositionalCLI_PRURLRewritesToReview(t *testing.T) {
	var got []string
	prevReview := runReviewForPositionalCLI
	prevLive := runLiveForPositionalCLI
	prevPreview := runPreviewForPositionalCLI
	t.Cleanup(func() {
		runReviewForPositionalCLI = prevReview
		runLiveForPositionalCLI = prevLive
		runPreviewForPositionalCLI = prevPreview
	})
	runReviewForPositionalCLI = func(args []string) error {
		got = append([]string{}, args...)
		return nil
	}
	runLiveForPositionalCLI = func([]string) { t.Fatal("live dispatch should not run for PR URL") }
	runPreviewForPositionalCLI = func([]string) { t.Fatal("preview dispatch should not run for PR URL") }

	runPositionalCLI([]string{"https://github.com/a/b/pull/7"})

	want := []string{"--pr", "https://github.com/a/b/pull/7"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("RunReview args = %v, want %v", got, want)
	}
}

func TestRunPositionalCLI_LiveURL(t *testing.T) {
	var liveCalled bool
	prevReview := runReviewForPositionalCLI
	prevLive := runLiveForPositionalCLI
	t.Cleanup(func() {
		runReviewForPositionalCLI = prevReview
		runLiveForPositionalCLI = prevLive
	})
	runLiveForPositionalCLI = func([]string) { liveCalled = true }

	runPositionalCLI([]string{"http://localhost:3000"})
	if !liveCalled {
		t.Fatal("expected live dispatch for dev-server URL")
	}
}

func TestRunPositionalCLI_PreviewHTML(t *testing.T) {
	dir := t.TempDir()
	htmlFile := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlFile, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var previewCalled bool
	prevPreview := runPreviewForPositionalCLI
	t.Cleanup(func() { runPreviewForPositionalCLI = prevPreview })
	runPreviewForPositionalCLI = func([]string) { previewCalled = true }

	runPositionalCLI([]string{htmlFile})
	if !previewCalled {
		t.Fatal("expected preview dispatch for .html file")
	}
}

func TestRunPositionalCLI_PlainFileReview(t *testing.T) {
	var got []string
	prevReview := runReviewForPositionalCLI
	t.Cleanup(func() { runReviewForPositionalCLI = prevReview })
	runReviewForPositionalCLI = func(args []string) error {
		got = append([]string{}, args...)
		return nil
	}

	runPositionalCLI([]string{"README.md"})
	if len(got) != 1 || got[0] != "README.md" {
		t.Fatalf("RunReview args = %v, want [README.md]", got)
	}
}
