package session

import (
	"reflect"
	"testing"
)

// The daemon merges replies written on disk by `crit comment --reply-to`
// and `crit fetch` into memory, where replies imported from crit-web before
// replies had ids sit without one.
func TestMergeDiskReplies(t *testing.T) {
	legacy := Reply{Body: "Done.", Author: "web reviewer"}
	cases := []struct {
		name        string
		mem, disk   []Reply
		want        []Reply
		wantChanged bool
	}{
		{
			// The agent answers "Done." while the web reviewer's legacy
			// "Done." is still id-less on disk: a new reply, kept apart.
			name:        "new reply with the body of a legacy reply",
			mem:         []Reply{legacy},
			disk:        []Reply{legacy, {ID: "rp_new", Body: "Done.", Author: "agent"}},
			want:        []Reply{legacy, {ID: "rp_new", Body: "Done.", Author: "agent"}},
			wantChanged: true,
		},
		{
			name:        "same author, legacy copy still id-less on disk",
			mem:         []Reply{legacy},
			disk:        []Reply{legacy, {ID: "rp_new", Body: "Done.", Author: "web reviewer"}},
			want:        []Reply{legacy, {ID: "rp_new", Body: "Done.", Author: "web reviewer"}},
			wantChanged: true,
		},
		{
			// crit fetch gave the legacy reply an id in place.
			name:        "legacy reply takes the id given on disk",
			mem:         []Reply{legacy},
			disk:        []Reply{{ID: "rp_web", Body: "Done.", Author: "web reviewer"}},
			want:        []Reply{{ID: "rp_web", Body: "Done.", Author: "web reviewer"}},
			wantChanged: true,
		},
		{
			name:        "one of two equal legacy replies took an id",
			mem:         []Reply{legacy, legacy},
			disk:        []Reply{{ID: "rp_web", Body: "Done.", Author: "web reviewer"}, legacy},
			want:        []Reply{{ID: "rp_web", Body: "Done.", Author: "web reviewer"}, legacy},
			wantChanged: true,
		},
		{
			name:        "several id-less disk-only replies all come in",
			mem:         []Reply{{ID: "rp_1", Body: "first"}},
			disk:        []Reply{{ID: "rp_1", Body: "first"}, legacy, legacy, {Body: "other", Author: "web reviewer"}},
			want:        []Reply{{ID: "rp_1", Body: "first"}, legacy, legacy, {Body: "other", Author: "web reviewer"}},
			wantChanged: true,
		},
		{
			name:        "only the id-less replies memory lacks come in",
			mem:         []Reply{legacy},
			disk:        []Reply{legacy, legacy},
			want:        []Reply{legacy, legacy},
			wantChanged: true,
		},
		{
			name: "nothing new",
			mem:  []Reply{legacy, {ID: "rp_1", Body: "x"}},
			disk: []Reply{legacy, {ID: "rp_1", Body: "x"}},
			want: []Reply{legacy, {ID: "rp_1", Body: "x"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := mergeDiskReplies(append([]Reply(nil), tc.mem...), tc.disk)
			if !reflect.DeepEqual(got, tc.want) || changed != tc.wantChanged {
				t.Errorf("mergeDiskReplies = %+v, %v\nwant %+v, %v", got, changed, tc.want, tc.wantChanged)
			}
		})
	}
}
