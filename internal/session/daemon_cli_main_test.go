package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestDirArgs(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		paths []string
		want  int
	}{
		{"empty", nil, 0},
		{"files only", []string{file}, 0},
		{"dirs only", []string{dir}, 1},
		{"mixed", []string{file, dir}, 1},
		{"nonexistent", []string{"/no/such/path"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirArgs(tt.paths)
			if len(got) != tt.want {
				t.Errorf("dirArgs(%v) returned %d dirs, want %d", tt.paths, len(got), tt.want)
			}
		})
	}
}

func TestBackgroundCleanupUsesGlobalStaleReviewDays(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	if err := os.WriteFile(
		filepath.Join(home, ".crit.config.json"),
		[]byte(`{"stale_review_days":90}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	reviewsDir := filepath.Join(home, ".crit", "reviews")
	if err := os.MkdirAll(reviewsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeReview := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(reviewsDir, name+".json")
		data, err := json.Marshal(CritJSON{
			Branch:    name,
			UpdatedAt: time.Now().Add(-age).UTC().Format(time.RFC3339),
			Files:     map[string]CritJSONFile{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	thirtyDaysOld := writeReview("thirty-days-old", 30*24*time.Hour)
	hundredDaysOld := writeReview("hundred-days-old", 100*24*time.Hour)

	backgroundCleanup()

	if _, err := os.Stat(thirtyDaysOld); err != nil {
		t.Fatalf("30-day-old review should remain with stale_review_days=90: %v", err)
	}
	if _, err := os.Stat(hundredDaysOld); !os.IsNotExist(err) {
		t.Fatalf("100-day-old review should be deleted with stale_review_days=90, stat error: %v", err)
	}
}

// stubIntegrationHooks swaps the hooks cmd/crit wires in and returns a pointer
// to the missing integrations passed to PrintMissingHintsFn.
func stubIntegrationHooks(t *testing.T, installed map[string]bool, missing []string) *[]string {
	t.Helper()
	oldInstalled, oldCheck, oldPrint := InstalledAgentsFn, CheckMissingIntegrationsFn, PrintMissingHintsFn
	t.Cleanup(func() {
		InstalledAgentsFn, CheckMissingIntegrationsFn, PrintMissingHintsFn = oldInstalled, oldCheck, oldPrint
	})
	var printed []string
	InstalledAgentsFn = func(string, string) map[string]bool { return installed }
	CheckMissingIntegrationsFn = func(string, string) []string { return missing }
	PrintMissingHintsFn = func(m []string) int { printed = m; return len(m) }
	return &printed
}

func TestHintMissingIntegrations(t *testing.T) {
	t.Run("prints missing integrations", func(t *testing.T) {
		t.Setenv("CRIT_NO_INTEGRATION_CHECK", "")
		printed := stubIntegrationHooks(t, nil, []string{"gemini"})
		HintMissingIntegrations()
		if len(*printed) != 1 || (*printed)[0] != "gemini" {
			t.Errorf("printed = %v, want [gemini]", *printed)
		}
	})
	t.Run("silent when an integration is installed", func(t *testing.T) {
		t.Setenv("CRIT_NO_INTEGRATION_CHECK", "")
		printed := stubIntegrationHooks(t, map[string]bool{"claude-code": true}, []string{"gemini"})
		HintMissingIntegrations()
		if *printed != nil {
			t.Errorf("printed = %v, want nothing", *printed)
		}
	})
	t.Run("disabled by CRIT_NO_INTEGRATION_CHECK", func(t *testing.T) {
		t.Setenv("CRIT_NO_INTEGRATION_CHECK", "1")
		printed := stubIntegrationHooks(t, nil, []string{"gemini"})
		HintMissingIntegrations()
		if *printed != nil {
			t.Errorf("printed = %v, want nothing", *printed)
		}
	})
}
