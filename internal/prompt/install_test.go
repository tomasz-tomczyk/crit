package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func TestInstallPrompts(t *testing.T) {
	dir := t.TempDir()
	if err := prompt.InstallPrompts(dir, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"on_finish_approved.md", "on_finish_unresolved.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	// Second install without force skips.
	if err := prompt.InstallPrompts(dir, false); err != nil {
		t.Fatal(err)
	}
}

func TestLoadStockTemplate(t *testing.T) {
	text, source, ok := prompt.LoadStockTemplate(prompt.HookFinishUnresolved, "files")
	if !ok || text == "" || source != "stock:on_finish_unresolved.md" {
		t.Fatalf("LoadStockTemplate = %q %q %v", text, source, ok)
	}
}
