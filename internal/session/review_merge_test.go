package session

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestMergeCritJSON3(t *testing.T) {
	comment := func(id, body string, replies ...string) Comment {
		c := Comment{ID: id, Body: body}
		for _, r := range replies {
			c.Replies = append(c.Replies, Reply{ID: r, Body: r})
		}
		return c
	}
	review := func(comments ...Comment) CritJSON {
		return CritJSON{Files: map[string]CritJSONFile{"a.go": {Comments: comments}}}
	}
	base := review(comment("c1", "one"), comment("c2", "two"))

	tests := []struct {
		name        string
		mine        CritJSON
		theirs      CritJSON
		wantBodies  []string
		wantReplies map[string][]string
	}{
		{
			name:       "theirs adds while mine edits",
			mine:       review(comment("c1", "one edited"), comment("c2", "two")),
			theirs:     review(comment("c1", "one"), comment("c2", "two"), comment("c3", "three")),
			wantBodies: []string{"one edited", "three", "two"},
		},
		{
			name:       "only theirs edits",
			mine:       base,
			theirs:     review(comment("c1", "one by theirs"), comment("c2", "two")),
			wantBodies: []string{"one by theirs", "two"},
		},
		{
			name:        "both edit, mine wins and keeps theirs reply",
			mine:        review(comment("c1", "one by mine"), comment("c2", "two")),
			theirs:      review(comment("c1", "one by theirs", "r-theirs"), comment("c2", "two")),
			wantBodies:  []string{"one by mine", "two"},
			wantReplies: map[string][]string{"c1": {"r-theirs"}},
		},
		{
			name:       "theirs deletes a comment mine left unchanged",
			mine:       base,
			theirs:     review(comment("c1", "one")),
			wantBodies: []string{"one"},
		},
		{
			name:       "mine deletes a comment theirs left unchanged",
			mine:       review(comment("c2", "two")),
			theirs:     base,
			wantBodies: []string{"two"},
		},
		{
			name:       "theirs deletes a comment mine edited",
			mine:       review(comment("c1", "one"), comment("c2", "two edited")),
			theirs:     review(comment("c1", "one")),
			wantBodies: []string{"one", "two edited"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeCritJSON3(base, tt.mine, tt.theirs)
			if err != nil {
				t.Fatal(err)
			}
			var bodies []string
			replies := map[string][]string{}
			for _, c := range got.Files["a.go"].Comments {
				bodies = append(bodies, c.Body)
				for _, r := range c.Replies {
					replies[c.ID] = append(replies[c.ID], r.ID)
				}
			}
			sort.Strings(bodies)
			if !reflect.DeepEqual(bodies, tt.wantBodies) {
				t.Errorf("bodies = %v, want %v", bodies, tt.wantBodies)
			}
			if tt.wantReplies == nil {
				tt.wantReplies = map[string][]string{}
			}
			if !reflect.DeepEqual(replies, tt.wantReplies) {
				t.Errorf("replies = %v, want %v", replies, tt.wantReplies)
			}
		})
	}
}

func TestMergeCritJSON3FieldsAndFiles(t *testing.T) {
	base := CritJSON{
		ShareURL:       "https://crit.md/r/old",
		Files:          map[string]CritJSONFile{"gone.go": {Comments: []Comment{{ID: "g"}}}},
		ReviewComments: []Comment{{ID: "r1", Body: "review"}},
	}
	mine := CloneCritJSON(base)
	mine.PendingRemoteDeletes = []RemoteRef{{CommentID: 7}}
	theirs := CloneCritJSON(base)
	theirs.ShareURL = "https://crit.md/r/new"
	theirs.Files = map[string]CritJSONFile{"new.go": {Comments: []Comment{{ID: "n"}}}}
	theirs.ReviewComments = append(theirs.ReviewComments, Comment{ID: "r2", Body: "added"})

	got, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShareURL != "https://crit.md/r/new" {
		t.Errorf("ShareURL = %q, theirs' change lost", got.ShareURL)
	}
	if len(got.PendingRemoteDeletes) != 1 {
		t.Errorf("PendingRemoteDeletes = %v, mine's change lost", got.PendingRemoteDeletes)
	}
	if _, ok := got.Files["gone.go"]; ok {
		t.Error("file theirs deleted came back")
	}
	if _, ok := got.Files["new.go"]; !ok {
		t.Error("file theirs added was dropped")
	}
	if len(got.ReviewComments) != 2 {
		t.Errorf("review comments = %+v", got.ReviewComments)
	}
}

func TestSaveCritJSONMergedKeepsConcurrentSave(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review")
	base := CritJSON{Files: map[string]CritJSONFile{"a.go": {Comments: []Comment{{ID: "c1", Body: "one"}}}}}
	if err := SaveCritJSON(critPath, base); err != nil {
		t.Fatal(err)
	}
	// Another writer saves while a long operation holds its copy.
	theirs := CloneCritJSON(base)
	theirs.ReviewComments = []Comment{{ID: "r1", Body: "from the daemon"}}
	if err := SaveCritJSON(critPath, theirs); err != nil {
		t.Fatal(err)
	}
	mine := CloneCritJSON(base)
	mine.Files["a.go"].Comments[0].GitLabNoteID = 42
	if err := SaveCritJSONMerged(critPath, base, mine); err != nil {
		t.Fatal(err)
	}
	got, err := readCritJSONFromDisk(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ReviewComments) != 1 {
		t.Errorf("concurrent review comment lost: %+v", got.ReviewComments)
	}
	if got.Files["a.go"].Comments[0].GitLabNoteID != 42 {
		t.Errorf("push result lost: %+v", got.Files["a.go"].Comments[0])
	}
}
