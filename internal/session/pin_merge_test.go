package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeExternalCritJSON_AdoptsPreviewPinRoute(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		ReviewType:  "preview",
		ReviewRound: 1,
		Files: []*FileEntry{
			{Path: "index.html", Status: "added", FileType: "code"},
		},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	ch := s.Subscribe()
	defer s.Unsubscribe(ch)

	cj := CritJSON{
		ReviewType:  "preview",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"index.html": {Status: "added"},
			"/preview-content": {
				Status: "added",
				Comments: []Comment{{
					ID: "c_pin", Body: "from CLI", PinNumber: 1,
					DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "h1", TagChain: []string{"H1"}},
				}},
			},
		},
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".crit", "review.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if !s.mergeExternalCritJSON() {
		t.Fatal("expected merge to notice the new pin")
	}
	comments := s.GetComments("/preview-content")
	if len(comments) != 1 || comments[0].Body != "from CLI" {
		t.Fatalf("comments = %+v", comments)
	}
	if s.fileByPathLocked("/preview-content").FileType != "live-route" {
		t.Fatal("adopted entry should be a live route")
	}
	select {
	case event := <-ch:
		if event.Type != "comments-changed" {
			t.Fatalf("event = %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for comments-changed")
	}
}

func TestMergeExternalCritJSON_IgnoresNewRouteOnCodeReview(t *testing.T) {
	dir := t.TempDir()
	s := &Session{
		RepoRoot:    dir,
		ReviewRound: 1,
		Files:       []*FileEntry{{Path: "main.go", Status: "modified"}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	cj := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {Status: "modified"},
			"/preview-content": {Comments: []Comment{{
				ID: "c_pin", Body: "stray",
				DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "h1"},
			}}},
		},
	}
	data, _ := json.Marshal(cj)
	path := filepath.Join(dir, ".crit", "review.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.mergeExternalCritJSON()
	if s.fileByPathLocked("/preview-content") != nil {
		t.Fatal("code reviews should not adopt pin routes")
	}
}

func TestAdoptExternalPinRoutesSkipsKnownAndPinless(t *testing.T) {
	s := &Session{
		ReviewType: "live",
		Files: []*FileEntry{
			{Path: "/dashboard", Status: "added", FileType: "live-route"},
		},
	}
	cj := &CritJSON{Files: map[string]CritJSONFile{
		"/dashboard": {Comments: []Comment{{
			ID: "c_known", DOMAnchor: &DOMAnchor{CSSSelector: "h1"},
		}}},
		"/plain": {Comments: []Comment{{ID: "c_plain", Body: "no pin"}}},
		"/new": {Comments: []Comment{{
			ID: "c_new", DOMAnchor: &DOMAnchor{CSSSelector: "button"},
		}}},
	}}
	s.mu.Lock()
	changed := s.adoptExternalPinRoutes(cj)
	s.mu.Unlock()
	if !changed {
		t.Fatal("expected the new pin route to be adopted")
	}
	if got := len(s.Files); got != 2 {
		t.Fatalf("files = %d, want the existing route plus /new", got)
	}
	added := s.fileByPathLocked("/new")
	if added == nil || added.Status != "added" || added.FileType != "live-route" {
		t.Fatalf("adopted = %+v", added)
	}
	if s.fileByPathLocked("/plain") != nil {
		t.Fatal("a route with no DOM pin should not be adopted")
	}
}
