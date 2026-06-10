package focus

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestResolveFocusFromPR(t *testing.T) {
	prevFetch := FetchPRByNumberHook
	prevStack := IsStackedPRHook
	t.Cleanup(func() {
		FetchPRByNumberHook = prevFetch
		IsStackedPRHook = prevStack
	})

	FetchPRByNumberHook = func(prNum int) (PRResolveInfo, error) {
		return PRResolveInfo{
			Number:      prNum,
			Title:       "Test PR",
			BaseRefOid:  "base1234567890",
			HeadRefOid:  "head1234567890",
			BaseRefName: "main",
			HeadRefName: "feature",
		}, nil
	}
	IsStackedPRHook = func(PRResolveInfo, vcs.VCS) bool { return false }

	dir := vcs.InitTestRepo(t)
	v := &vcs.GitVCS{}
	// Seed objects locally so EnsureSHAFetched short-circuits.
	base := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	head := vcs.CommitAtForTest(t, dir, "pr.txt", "x", "pr change")

	FetchPRByNumberHook = func(prNum int) (PRResolveInfo, error) {
		return PRResolveInfo{
			Number:      prNum,
			Title:       "Test PR",
			BaseRefOid:  base,
			HeadRefOid:  head,
			BaseRefName: "main",
			HeadRefName: "feature",
		}, nil
	}

	// remoteFiles skips EnsureSHAFetched; this test wires PR hooks, not git fetch.
	f, err := ResolveFocus("42", "", "", true, v, dir)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.PRNumber != 42 || f.HeadSHA != head {
		t.Errorf("got %+v", f)
	}
}

func TestSetPRResolveHooks(t *testing.T) {
	SetPRResolveHooks(
		func(int) (PRResolveInfo, error) { return PRResolveInfo{Number: 1}, nil },
		func(PRResolveInfo, vcs.VCS) bool { return true },
	)
	if FetchPRByNumberHook == nil || IsStackedPRHook == nil {
		t.Fatal("hooks not wired")
	}
}
