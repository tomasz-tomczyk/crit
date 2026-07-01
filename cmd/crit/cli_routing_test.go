package main

import (
	"testing"
)

func TestRoutePositionalArgs(t *testing.T) {
	cases := []struct {
		args []string
		want positionalRoute
	}{
		{[]string{"https://github.com/a/b/pull/295"}, positionalRoutePRReview},
		{[]string{"https://github.com/a/b/pull/295/files"}, positionalRoutePRReview},
		{[]string{"http://localhost:3000"}, positionalRouteLive},
		{[]string{"https://example.com/app"}, positionalRouteLive},
		{[]string{"README.md"}, positionalRouteReview},
		{[]string{"http://a.com", "README.md"}, positionalRouteReview},
	}
	for _, c := range cases {
		t.Run(c.args[0], func(t *testing.T) {
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
