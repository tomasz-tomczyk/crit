package focus

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestResolveFocusFromPR_UsesMergeBaseNotBaseTip is the regression guard for the
// reported bug: `crit --pr` on a branch that isn't rebased onto its base must
// diff from the merge-base (matching GitHub's base...head), not from the base
// branch tip (which folds in unrelated base-branch drift as spurious changes).
func TestResolveFocusFromPR_UsesMergeBaseNotBaseTip(t *testing.T) {
	vcs.ClearGitEnvForTest(t) // robust under git hooks that export GIT_DIR

	prevFetch := FetchPRByNumberHook
	prevStack := IsStackedPRHook
	t.Cleanup(func() {
		FetchPRByNumberHook = prevFetch
		IsStackedPRHook = prevStack
	})

	// Drifted repo: the base branch advances past the PR's fork point.
	//   main:    C0 ── C1(forkpoint) ── M1(drift, base tip)
	//                     └── F1(PR head)
	dir := vcs.InitTestRepo(t)
	forkpoint := vcs.CommitAtForTest(t, dir, "base.txt", "base\n", "forkpoint")
	vcs.GitRun(t, dir, "checkout", "-b", "feature")
	prHead := vcs.CommitAtForTest(t, dir, "feature.txt", "feat\n", "pr work")
	vcs.GitRun(t, dir, "checkout", "main")
	baseTip := vcs.CommitAtForTest(t, dir, "drift.txt", "drift\n", "base branch drift")

	FetchPRByNumberHook = func(int) (PRResolveInfo, error) {
		return PRResolveInfo{
			Number:      7,
			Title:       "drifted PR",
			URL:         "https://github.com/o/r/pull/7",
			BaseRefName: "main",
			HeadRefName: "feature",
			BaseRefOid:  baseTip, // GitHub reports the base branch TIP
			HeadRefOid:  prHead,
		}, nil
	}
	IsStackedPRHook = func(PRResolveInfo, vcs.VCS) bool { return false }

	// remoteFiles=true skips EnsureSHAFetched; all objects already exist locally,
	// so the merge-base is computed against the real repo.
	f, err := ResolveFocus("7", "", "", true, &vcs.GitVCS{}, dir)
	if err != nil {
		t.Fatalf("ResolveFocus: %v", err)
	}
	if f == nil {
		t.Fatal("ResolveFocus returned nil focus")
	}
	if f.BaseSHA != forkpoint {
		t.Errorf("Focus.BaseSHA = %s, want merge-base %s; base tip %s would reintroduce the bug",
			f.BaseSHA, forkpoint, baseTip)
	}
	if f.HeadSHA != prHead {
		t.Errorf("Focus.HeadSHA = %s, want %s", f.HeadSHA, prHead)
	}
}
