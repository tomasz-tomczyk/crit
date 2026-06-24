package session

import (
	"strings"
	"testing"
)

func TestRunReview_InvalidSessionID(t *testing.T) {
	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })

	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{SessionID: "not-a-valid-id"}, nil
	}

	err := RunReview(nil)
	if err == nil {
		t.Fatal("expected error for invalid session ID")
	}
	if !strings.Contains(err.Error(), "invalid session ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}
