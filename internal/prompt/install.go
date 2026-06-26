package prompt

import (
	"fmt"
	"os"
	"path/filepath"

	integrationassets "github.com/tomasz-tomczyk/crit/integrations"
)

var stockPromptFiles = []string{
	"on_finish_approved.md",
	"on_finish_unresolved.md",
}

// InstallPrompts copies stock finish templates into destDir (typically ~/.crit/prompts or .crit/prompts).
func InstallPrompts(destDir string, force bool) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", destDir, err)
	}
	for _, name := range stockPromptFiles {
		src := "integrations/prompts/" + name
		data, err := integrationassets.FS.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading stock prompt %s: %w", name, err)
		}
		dst := filepath.Join(destDir, name)
		if !force {
			if _, err := os.Stat(dst); err == nil {
				fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", dst)
				continue
			}
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", dst, err)
		}
		fmt.Printf("  Installed: %s\n", dst)
	}
	return nil
}
