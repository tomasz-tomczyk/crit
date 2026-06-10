package vcs

import (
	"context"
	"testing"
)

type fakeFetchVCS struct {
	name    string
	hasSeq  []bool
	hasCall int
}

func (f *fakeFetchVCS) Name() string { return f.name }
func (f *fakeFetchVCS) HasObject(_, _ string) bool {
	if len(f.hasSeq) > f.hasCall {
		v := f.hasSeq[f.hasCall]
		f.hasCall++
		return v
	}
	return false
}
func (f *fakeFetchVCS) RepoRoot() (string, error)        { return "", nil }
func (f *fakeFetchVCS) CurrentBranch() string            { return "main" }
func (f *fakeFetchVCS) DefaultBranch() string            { return "main" }
func (f *fakeFetchVCS) SetDefaultBranchOverride(string)  {}
func (f *fakeFetchVCS) GetDefaultBranchOverride() string { return "" }
func (f *fakeFetchVCS) DefaultBaseRef() string           { return "main" }
func (f *fakeFetchVCS) MergeBase(string) (string, error) { return "", nil }
func (f *fakeFetchVCS) ChangedFilesOnDefaultInDir(string) ([]FileChange, error) {
	return nil, nil
}
func (f *fakeFetchVCS) ChangedFilesFromBaseInDir(string, string) ([]FileChange, error) {
	return nil, nil
}
func (f *fakeFetchVCS) ChangedFilesScoped(string, string) ([]FileChange, error) {
	return nil, nil
}
func (f *fakeFetchVCS) ChangedFilesForCommit(string, string) ([]FileChange, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffUnified(string, string, string, bool) ([]DiffHunk, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffUnifiedCtx(context.Context, string, string, string, bool) ([]DiffHunk, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffScoped(string, string, string, string, bool) ([]DiffHunk, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffForCommit(string, string, string, bool) ([]DiffHunk, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffUnifiedNewFile(string) ([]DiffHunk, error) { return nil, nil }
func (f *fakeFetchVCS) CommitLog(string, string, string) ([]CommitInfo, error) {
	return nil, nil
}
func (f *fakeFetchVCS) WorkingTreeFingerprint() string              { return "" }
func (f *fakeFetchVCS) UntrackedFiles(string) ([]FileChange, error) { return nil, nil }
func (f *fakeFetchVCS) AllTrackedFiles(string) ([]string, error)    { return nil, nil }
func (f *fakeFetchVCS) RemoteBranches(string) ([]string, error)     { return nil, nil }
func (f *fakeFetchVCS) DiffNumstat(string, string) (map[string]NumstatEntry, error) {
	return nil, nil
}
func (f *fakeFetchVCS) UserName() string { return "" }
func (f *fakeFetchVCS) FileContentAtRef(string, string, string) (string, error) {
	return "", nil
}
func (f *fakeFetchVCS) ChangedFilesBetweenSHAs(string, string, string) ([]FileChange, error) {
	return nil, nil
}
func (f *fakeFetchVCS) FileDiffBetweenSHAs(string, string, string, string, string, bool) ([]DiffHunk, error) {
	return nil, nil
}
func (f *fakeFetchVCS) ReadFileAtSHA(string, string, string) ([]byte, error) { return nil, nil }
func (f *fakeFetchVCS) FileStatusInRepo(string, string, string) string       { return "" }
func (f *fakeFetchVCS) HasStagingArea() bool                                 { return true }
func (f *fakeFetchVCS) SkipDirNames() []string                               { return nil }

func TestEnsureSHAFetched_AlreadyPresent(t *testing.T) {
	v := &fakeFetchVCS{name: "git", hasSeq: []bool{true}}
	if err := ensureSHAFetched(v, "abc", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSHAFetched_StillMissingAfterFetch(t *testing.T) {
	v := &fakeFetchVCS{name: "git"}
	if err := ensureSHAFetched(v, "deadbeef", t.TempDir(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSHAFetched_NilVCS(t *testing.T) {
	if err := ensureSHAFetched(nil, "abc", "", ""); err != nil {
		t.Errorf("nil vcs should no-op, got %v", err)
	}
}

func TestEnsureSHAFetched_UnsupportedVCS(t *testing.T) {
	v := &fakeFetchVCS{name: "fossil"}
	if err := ensureSHAFetched(v, "abc", t.TempDir(), ""); err == nil {
		t.Fatal("expected error for unsupported vcs")
	}
}
