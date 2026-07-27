package reviewpath

import "path/filepath"

// FromOutputDir resolves an output directory to its v4 review identity folder.
func FromOutputDir(outputDir string) (string, error) {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, ".crit"), nil
}
