package vcs

import (
	"reflect"
	"testing"
)

func TestSplitCommitRange_Invalid(t *testing.T) {
	_, _, ok := SplitCommitRange("not-a-range")
	if ok {
		t.Error("expected false for invalid range")
	}
	_, _, ok = SplitCommitRange("abc..")
	if ok {
		t.Error("expected false for empty head")
	}
}

func TestChangedFilesOnDefaultInDir_Git(t *testing.T) {
	dir := InitTestRepo(t)
	writeFileForTest(t, dir+"/dirty.txt", "uncommitted")
	files, err := (&GitVCS{}).ChangedFilesOnDefaultInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "dirty.txt", Status: "untracked"}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("ChangedFilesOnDefaultInDir() = %+v, want %+v", files, want)
	}
}

func TestFileStatusInRepo_Git(t *testing.T) {
	dir := InitTestRepo(t)
	writeFileForTest(t, dir+"/new.txt", "x")
	base := GitRun(t, dir, "rev-parse", "HEAD")
	status := (&GitVCS{}).FileStatusInRepo("new.txt", base, dir)
	if status != "untracked" {
		t.Errorf("FileStatusInRepo(untracked) = %q, want untracked", status)
	}
}
