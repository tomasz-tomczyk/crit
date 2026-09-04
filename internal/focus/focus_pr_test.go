package focus

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestResolveFocusFromPR(t *testing.T) {
	prevFetch := FetchPRHook
	prevStack := IsStackedPRHook
	t.Cleanup(func() {
		FetchPRHook = prevFetch
		IsStackedPRHook = prevStack
	})

	IsStackedPRHook = func(ChangeResolveInfo, vcs.VCS) bool { return false }

	dir := vcs.InitTestRepo(t)
	v := &vcs.GitVCS{}
	// Seed objects locally so EnsureSHAFetched short-circuits.
	base := vcs.GitRun(t, dir, "rev-parse", "HEAD")
	head := vcs.CommitAtForTest(t, dir, "pr.txt", "x", "pr change")

	FetchPRHook = func(spec string) (ChangeResolveInfo, error) {
		return ChangeResolveInfo{
			Number:          42,
			Title:           "Test PR",
			BaseRefOid:      base,
			HeadRefOid:      head,
			BaseRefName:     "main",
			HeadRefName:     "feature",
			BaseRepoProject: "myorg/repo-b",
		}, nil
	}

	// remoteFiles skips EnsureSHAFetched; this test wires PR hooks, not git fetch.
	f, err := ResolveFocus(ChangeSpec{Forge: "github", Value: "https://github.com/myorg/repo-b/pull/42"}, "", "", true, v, dir)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.Forge != "github" || f.ChangeNumber != 42 || f.HeadSHA != head {
		t.Errorf("got %+v", f)
	}
	if f.RemoteBaseProject != "myorg/repo-b" {
		t.Errorf("RemoteBaseProject=%q want myorg/repo-b", f.RemoteBaseProject)
	}
}

func TestSetPRResolveHooks(t *testing.T) {
	SetPRResolveHooks(
		func(string) (ChangeResolveInfo, error) { return ChangeResolveInfo{Number: 1}, nil },
		func(ChangeResolveInfo, vcs.VCS) bool { return true },
	)
	if FetchPRHook == nil || IsStackedPRHook == nil {
		t.Fatal("hooks not wired")
	}
}
