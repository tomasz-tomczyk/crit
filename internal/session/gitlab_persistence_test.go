package session

import (
	"testing"
)

func TestReconcilePendingRemoteDeletesDoesNotResurrectDrained(t *testing.T) {
	old := RemoteRef{Forge: "gitlab", CommentID: 1, ThreadID: "old"}
	fresh := RemoteRef{Forge: "gitlab", CommentID: 2, ThreadID: "fresh"}
	got := reconcilePendingRemoteDeletes(
		[]RemoteRef{old, fresh}, nil,
		map[RemoteRef]struct{}{old: {}},
	)
	if len(got) != 1 || got[0] != fresh {
		t.Fatalf("reconciled = %+v, want only fresh", got)
	}
}
