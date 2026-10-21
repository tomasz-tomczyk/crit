package session

import (
	"errors"
	"os"
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
	if _, err := SaveCritJSONMerged(critPath, base, mine); err != nil {
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

// Push stamps IDs on a comment while the user resolves it and deletes a
// reply: both sides' field changes survive and the reply stays deleted.
func TestMergeCritJSON3CommentFieldsAndReplies(t *testing.T) {
	c := Comment{ID: "c1", Body: "one", Replies: []Reply{{ID: "r1", Body: "a"}, {ID: "r2", Body: "b"}}}
	base := CritJSON{Files: map[string]CritJSONFile{"a.go": {Comments: []Comment{c}}}}
	mine := CloneCritJSON(base)
	mine.Files["a.go"].Comments[0].GitLabNoteID = 99
	theirs := CloneCritJSON(base)
	theirs.Files["a.go"].Comments[0].Resolved = true
	theirs.Files["a.go"].Comments[0].Replies = theirs.Files["a.go"].Comments[0].Replies[:1]

	got, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	merged := got.Files["a.go"].Comments[0]
	if merged.GitLabNoteID != 99 || !merged.Resolved {
		t.Errorf("merged = %+v, want note ID 99 and resolved", merged)
	}
	if len(merged.Replies) != 1 || merged.Replies[0].ID != "r1" {
		t.Errorf("replies = %+v, want only r1", merged.Replies)
	}
}

func TestMergeCritJSON3PendingRemoteDeletes(t *testing.T) {
	r1, r2, r3 := RemoteRef{CommentID: 1}, RemoteRef{CommentID: 2}, RemoteRef{CommentID: 3}
	base := CritJSON{PendingRemoteDeletes: []RemoteRef{r1}}
	mine := CritJSON{PendingRemoteDeletes: []RemoteRef{r3}}       // drained r1, queued r3
	theirs := CritJSON{PendingRemoteDeletes: []RemoteRef{r1, r2}} // daemon queued r2
	got, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.PendingRemoteDeletes, []RemoteRef{r2, r3}) {
		t.Errorf("pending deletes = %v, want [r2 r3]", got.PendingRemoteDeletes)
	}
}

func TestMergeCritJSON3IDlessAndDuplicateComments(t *testing.T) {
	base := CritJSON{ReviewComments: []Comment{{ID: "d", Body: "first"}}}
	mine := CloneCritJSON(base)
	mine.ReviewComments = append(mine.ReviewComments, Comment{Body: "mine, no id"})
	theirs := CloneCritJSON(base)
	theirs.ReviewComments = append(theirs.ReviewComments, Comment{Body: "theirs, no id"}, Comment{ID: "d", Body: "duplicate"})
	got, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, c := range got.ReviewComments {
		bodies = append(bodies, c.Body)
	}
	if !reflect.DeepEqual(bodies, []string{"first", "mine, no id", "theirs, no id"}) {
		t.Errorf("bodies = %v", bodies)
	}
}

// A file theirs deleted keeps only the comments mine changed.
func TestMergeCritJSON3FileDeletedByTheirs(t *testing.T) {
	base := CritJSON{Files: map[string]CritJSONFile{"a.go": {Status: "modified", Comments: []Comment{{ID: "c1"}, {ID: "c2"}}}}}
	mine := CloneCritJSON(base)
	mine.Files["a.go"].Comments[0].GitLabNoteID = 5
	got, err := mergeCritJSON3(base, mine, CritJSON{})
	if err != nil {
		t.Fatal(err)
	}
	f := got.Files["a.go"]
	if len(f.Comments) != 1 || f.Comments[0].ID != "c1" || f.Status != "modified" {
		t.Errorf("file = %+v, want only c1 with status kept", f)
	}
}

func TestSaveCritJSONMergedRefusesRemovedReview(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review")
	base := CritJSON{Branch: "feature", Files: map[string]CritJSONFile{"a.go": {Comments: []Comment{{ID: "c1"}}}}}
	if err := SaveCritJSON(critPath, base); err != nil {
		t.Fatal(err)
	}
	if err := ClearReviewFolder(critPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveCritJSONMerged(critPath, base, base); !errors.Is(err, ErrReviewRemoved) {
		t.Fatalf("err = %v, want ErrReviewRemoved", err)
	}
	if _, err := os.Stat(critPath); !os.IsNotExist(err) {
		t.Fatalf("review folder recreated: %v", err)
	}
}

// The second of two saves must not undo a change the first one kept.
func TestSaveCritJSONMergedTwiceKeepsTheirs(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review")
	base := CritJSON{Files: map[string]CritJSONFile{"a.go": {Comments: []Comment{{ID: "c1", Body: "one"}}}}}
	if err := SaveCritJSON(critPath, base); err != nil {
		t.Fatal(err)
	}
	theirs := CloneCritJSON(base)
	theirs.Files["a.go"].Comments[0].Resolved = true
	if err := SaveCritJSON(critPath, theirs); err != nil {
		t.Fatal(err)
	}

	mine := CloneCritJSON(base)
	mine.Files["a.go"].Comments[0].LastPushedBodyHash = "h"
	merged, err := SaveCritJSONMerged(critPath, base, mine)
	if err != nil {
		t.Fatal(err)
	}
	next := CloneCritJSON(merged)
	next.Files["a.go"].Comments[0].GitLabNoteID = 7
	if _, err := SaveCritJSONMerged(critPath, merged, next); err != nil {
		t.Fatal(err)
	}
	got, err := readCritJSONFromDisk(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Files["a.go"].Comments[0]; !c.Resolved || c.GitLabNoteID != 7 || c.LastPushedBodyHash != "h" {
		t.Errorf("comment = %+v", c)
	}
}

// The user edits a reply while push stamps it: both changes survive.
func TestMergeCritJSON3ReplyChangedOnBothSides(t *testing.T) {
	base := CritJSON{ReviewComments: []Comment{{ID: "c1", Replies: []Reply{{ID: "r1", Body: "old"}}}}}
	mine := CloneCritJSON(base)
	mine.ReviewComments[0].Replies[0].GitLabNoteID = 7
	theirs := CloneCritJSON(base)
	theirs.ReviewComments[0].Replies[0].Body = "new"
	got, err := mergeCritJSON3(base, mine, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.ReviewComments[0].Replies[0]; r.Body != "new" || r.GitLabNoteID != 7 {
		t.Errorf("reply = %+v, want body new and note 7", r)
	}
}
