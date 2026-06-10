//go:build integration

package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/auth"
)

type (
	webComment    = WebComment
	webReply      = WebReply
	tokenResponse = auth.TokenResponse
)

var saveAuthSession = auth.SaveAuthSession

// readCritJSON reads .crit/review.json from dir (integration test helper).
func readCritJSON(t *testing.T, dir string) CritJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	if err != nil {
		t.Fatalf("reading .crit/review.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("parsing .crit/review.json: %v", err)
	}
	return cj
}
