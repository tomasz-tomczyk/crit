//go:build integration

package github

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func skipIfNoGH(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not installed")
	}
	if err := exec.Command("gh", "auth", "status").Run(); err != nil {
		t.Skip("gh not authenticated")
	}
}

func TestPRIntegration_FetchByNumber(t *testing.T) {
	skipIfNoGH(t)
	prStr := os.Getenv("CRIT_TEST_PR")
	if prStr == "" {
		t.Skip("set CRIT_TEST_PR=<num> to run this test against a real PR")
	}
	prNum, err := strconv.Atoi(prStr)
	if err != nil {
		t.Fatalf("invalid CRIT_TEST_PR: %v", err)
	}
	info, err := fetchPRByNumber(prNum)
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != prNum {
		t.Errorf("got %d, want %d", info.Number, prNum)
	}
	if info.HeadRefOid == "" {
		t.Errorf("HeadRefOid empty")
	}
	if info.BaseRefOid == "" {
		t.Errorf("BaseRefOid empty")
	}
}

func TestPRIntegration_IsStackedPR_RealPR(t *testing.T) {
	skipIfNoGH(t)
	if pr := os.Getenv("CRIT_TEST_STACKED_PR"); pr != "" {
		prNum, _ := strconv.Atoi(pr)
		info, err := fetchPRByNumber(prNum)
		if err != nil {
			t.Fatal(err)
		}
		if !IsStackedPR(info, &vcs.GitVCS{}) {
			t.Errorf("PR #%d expected stacked, got not stacked (base=%q, default=%q)",
				prNum, info.BaseRefName, (&vcs.GitVCS{}).DefaultBranch())
		}
	}
	if pr := os.Getenv("CRIT_TEST_NONSTACKED_PR"); pr != "" {
		prNum, _ := strconv.Atoi(pr)
		info, err := fetchPRByNumber(prNum)
		if err != nil {
			t.Fatal(err)
		}
		if IsStackedPR(info, &vcs.GitVCS{}) {
			t.Errorf("PR #%d expected not stacked, got stacked (base=%q, default=%q)",
				prNum, info.BaseRefName, (&vcs.GitVCS{}).DefaultBranch())
		}
	}
}
