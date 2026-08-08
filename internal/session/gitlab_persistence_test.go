package session

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestCritJSONMigratesLegacyProviderDeleteQueues(t *testing.T) {
	var cj CritJSON
	err := json.Unmarshal([]byte(`{
		"files": {},
		"pending_github_deletes": [7],
		"pending_gitlab_deletes": [{"note_id": 8, "discussion_id": "d8"}]
	}`), &cj)
	if err != nil {
		t.Fatal(err)
	}
	want := []RemoteRef{
		{Forge: "github", CommentID: 7},
		{Forge: "gitlab", CommentID: 8, ThreadID: "d8"},
	}
	if !reflect.DeepEqual(cj.PendingRemoteDeletes, want) {
		t.Fatalf("migrated deletes = %+v, want %+v", cj.PendingRemoteDeletes, want)
	}
	encoded, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || !json.Valid(encoded) {
		t.Fatalf("invalid migrated JSON: %q", encoded)
	}
	for _, legacyField := range []string{"pending_github_deletes", "pending_gitlab_deletes"} {
		if strings.Contains(string(encoded), legacyField) {
			t.Fatalf("new review JSON re-emitted legacy field %q: %s", legacyField, encoded)
		}
	}
}
