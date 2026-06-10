package focus

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// fakeStackVCS implements vcs.VCS just enough for ResolveFocus remote tests.
type fakeStackVCS struct {
	vcs.VCS
	name     string
	hasCalls int
	hasSeq   []bool
	def      string
}

func (f *fakeStackVCS) DefaultBranch() string { return f.def }
func (f *fakeStackVCS) Name() string {
	if f.name != "" {
		return f.name
	}
	return "git"
}
func (f *fakeStackVCS) HasObject(_, _ string) bool {
	if f.hasSeq == nil {
		return false
	}
	if f.hasCalls < len(f.hasSeq) {
		v := f.hasSeq[f.hasCalls]
		f.hasCalls++
		return v
	}
	return false
}

func TestResolveFocus_RangeRemoteSkipsHasObject(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: nil}
	f, err := ResolveFocus("", "abc..def", "", true, v, t.TempDir())
	if err != nil {
		t.Fatalf("expected --remote to skip HasObject, got err: %v", err)
	}
	if f == nil || f.HeadSHA != "def" {
		t.Errorf("got %+v", f)
	}
}

func TestResolveFocus_RangeNonRemoteEnforcesHasObject(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: nil}
	_, err := ResolveFocus("", "abc..def", "", false, v, t.TempDir())
	if err == nil {
		t.Fatal("expected error from missing local SHA")
	}
}

var _ vcs.VCS = (*fakeStackVCS)(nil)
