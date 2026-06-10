package share

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestExportWrappers_Smoke(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	cj := session.CritJSON{
		Files:       map[string]session.CritJSONFile{},
		ShareURL:    "https://example.com/r/tok",
		DeleteToken: "del",
	}
	writeCritJSONForTest(t, dir, cj)

	if err := CheckShareAllowed(critPath); err != nil {
		t.Fatalf("CheckShareAllowed: %v", err)
	}
	if err := CheckGitHubSyncAllowed(cj, "push"); err != nil {
		t.Fatalf("CheckGitHubSyncAllowed: %v", err)
	}
	loaded, ok, err := LoadExistingShareCfg(critPath, []string{"a.go"})
	if err != nil || !ok {
		t.Fatalf("LoadExistingShareCfg: ok=%v err=%v", ok, err)
	}
	_ = loaded

	hash := ComputeShareHash(nil, nil)
	if hash == "" {
		t.Error("ComputeShareHash returned empty")
	}
	if err := UpdateShareState(critPath, hash, 1); err != nil {
		t.Fatal(err)
	}
	if err := PersistShareState(critPath, "https://x/r/t", "del", "layer", "", "", "private"); err != nil {
		t.Fatal(err)
	}
	if err := ClearShareState(critPath); err != nil {
		t.Fatal(err)
	}

	comments := []ShareComment{{Body: "x", File: "preview.html"}}
	RemapPreviewCommentFiles(comments)
	if comments[0].File != session.PreviewMainHTMLKey {
		t.Errorf("remap preview file = %q, want %q", comments[0].File, session.PreviewMainHTMLKey)
	}

	cfg := LoadShareConfig()
	_ = ResolveShareURL("", cfg, "https://fallback")
	_ = ResolveAuthToken(cfg)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			json.NewEncoder(w).Encode(map[string]any{"url": "http://example.com/r/tok", "review_round": 2, "changed": true})
		}
	}))
	defer srv.Close()
	upsertCfg := session.CritJSON{
		ShareURL:      srv.URL + "/r/tok",
		DeleteToken:   "dt",
		LastShareHash: "stale",
		ReviewRound:   1,
	}
	result, err := UpsertShareToWeb(upsertCfg, []ShareFile{{Path: "a.md", Content: "new"}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("expected changed=true from upsert export")
	}
}
