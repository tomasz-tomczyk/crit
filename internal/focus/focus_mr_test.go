package focus

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestResolveFocusFromMR(t *testing.T) {
	previousFetch := FetchMRHook
	previousStack := IsStackedMRHook
	t.Cleanup(func() {
		FetchMRHook = previousFetch
		IsStackedMRHook = previousStack
	})

	const mrURL = "https://gitlab.company.test/platform/tools/crit/-/merge_requests/23"
	FetchMRHook = func(spec string) (ChangeResolveInfo, error) {
		if spec != mrURL {
			t.Fatalf("MR spec = %q, want %q", spec, mrURL)
		}
		return ChangeResolveInfo{
			URL:         mrURL,
			Number:      23,
			Title:       "Test MR",
			BaseRefOid:  "base1234567890",
			HeadRefOid:  "head1234567890",
			BaseRefName: "main",
			HeadRefName: "feature",
		}, nil
	}
	IsStackedMRHook = func(ChangeResolveInfo, vcs.VCS) bool { return false }

	f, err := ResolveFocus(ChangeSpec{Forge: "gitlab", Value: mrURL}, "", "", true, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.Forge != "gitlab" || f.ChangeNumber != 23 || f.MRURL != mrURL || f.HeadSHA != "head1234567890" {
		t.Fatalf("focus = %+v", f)
	}
}
