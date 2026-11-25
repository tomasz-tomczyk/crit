package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/comment"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	sesspkg "github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func getAllFileComments(t *testing.T, s *Server, url string) map[string][]Comment {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string][]Comment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestHandleAllFileComments_GroupsByPathAndHidesOtherFocus(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Files[0].Comments = []Comment{
		{ID: "c1", Body: "visible"},
		{ID: "c2", Body: "from a PR focus", FocusKey: "pr:github.com/o/r#7"},
	}
	sess.Files = append(sess.Files,
		&FileEntry{Path: "lazy.go", Status: "added", FileType: "code", Lazy: true,
			Comments: []Comment{{ID: "c3", Body: "on a lazy file", Resolved: true}}},
		&FileEntry{Path: "empty.go", Status: "added", FileType: "code"},
		&FileEntry{Path: "hidden.go", Status: "added", FileType: "code",
			Comments: []Comment{{ID: "c4", FocusKey: "range:a..b"}}},
	)

	got := getAllFileComments(t, s, "/api/files/comments")

	if len(got) != 2 {
		t.Fatalf("expected 2 paths, got %v", got)
	}
	if cs := got["test.md"]; len(cs) != 1 || cs[0].ID != "c1" {
		t.Errorf("test.md: expected [c1], got %+v", cs)
	}
	if cs := got["lazy.go"]; len(cs) != 1 || cs[0].ID != "c3" || !cs[0].Resolved {
		t.Errorf("lazy.go: expected resolved c3, got %+v", cs)
	}
}

func TestRunComment_VisibleInRemoteReview(t *testing.T) {
	tests := []struct {
		name, forge, host string
		scopeFlag         []string
	}{
		{name: "GitLab MR", forge: "gitlab", host: "gitlab.com"},
		{name: "self-hosted GitLab MR with explicit scope", forge: "gitlab", host: "gitlab.example.com", scopeFlag: []string{"--scope", "layer"}},
		{name: "GitHub Enterprise PR", forge: "github", host: "github.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetHome(t, t.TempDir())
			s, sess := newTestServer(t)
			t.Chdir(sess.RepoRoot)
			sess.Mode = "git"
			sess.Focus = sesspkg.Focus{
				Kind:              sesspkg.FocusRange,
				Forge:             tt.forge,
				ChangeNumber:      17,
				RemoteBaseProject: "group/project",
				RemoteHost:        tt.host,
				DiffScope:         sesspkg.DiffScopeLayer,
			}
			srv := httptest.NewServer(s)
			t.Cleanup(srv.Close)
			if err := daemon.WriteSessionFile(sess.SessionKey, daemon.SessionEntry{
				PID: os.Getpid(), Port: srv.Listener.Addr().(*net.TCPAddr).Port,
				CWD: sess.RepoRoot, ReviewPath: sess.CritJSONPath(),
			}); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--session", sess.SessionKey}, tt.scopeFlag...)
			if err := comment.RunComment(append(args, "test.md:2", "line feedback")); err != nil {
				t.Fatal(err)
			}
			if !sess.SyncCommentsFromDisk() {
				t.Fatal("expected the CLI comment to be loaded from the review file")
			}
			got := getAllFileComments(t, s, "/api/files/comments")["test.md"]
			if len(got) != 1 || got[0].Body != "line feedback" || got[0].StartLine != 2 {
				t.Fatalf("visible comments = %+v, want line feedback on line 2", got)
			}
		})
	}
}

func TestHandleAllFileComments_RoundFilter(t *testing.T) {
	tests := []struct {
		name string
		mode string
		url  string
		want []string
	}{
		{"files mode filters by round", "files", "/api/files/comments?round=2", []string{"c1", "c2"}},
		{"files mode without round", "files", "/api/files/comments", []string{"c1", "c2", "c3"}},
		{"git mode ignores round", "git", "/api/files/comments?round=2", []string{"c1", "c2", "c3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, sess := newRoundsTestServer(t)
			sess.Mode = tt.mode
			sess.Files[0].Comments = []Comment{
				{ID: "c1", ReviewRound: 1},
				{ID: "c2", ReviewRound: 2},
				{ID: "c3", ReviewRound: 3},
			}

			got := getAllFileComments(t, s, tt.url)

			var ids []string
			for _, c := range got["test.md"] {
				ids = append(ids, c.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestHandleAllFileComments_RoundFilterDropsEmptyFiles(t *testing.T) {
	s, sess := newRoundsTestServer(t)
	sess.Files[0].Comments = []Comment{{ID: "c3", ReviewRound: 3}}

	got := getAllFileComments(t, s, "/api/files/comments?round=2")

	if _, ok := got["test.md"]; ok {
		t.Errorf("expected test.md to be left out, got %v", got)
	}
}

func TestHandleAllFileComments_InvalidRound(t *testing.T) {
	s, _ := newRoundsTestServer(t)
	for _, url := range []string{"/api/files/comments?round=0", "/api/files/comments?round=abc"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 400 {
			t.Errorf("%s: expected 400, got %d", url, w.Code)
		}
	}
}

func TestHandleAllFileComments_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/files/comments", nil))
	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
