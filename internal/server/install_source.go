package server

import (
	"os"
	"path/filepath"
	"strings"
)

// installationSource describes the package manager that owns the running
// binary. It is deliberately conservative: an unrecognised path is returned
// as unknown rather than guessing an update command.
const (
	installationSourceUnknown  = "unknown"
	installationSourceHomebrew = "homebrew"
	installationSourceGo       = "go"
	installationSourceNix      = "nix"
)

var executablePath = os.Executable

func detectInstallationSource() string {
	path, err := executablePath()
	if err != nil {
		return installationSourceUnknown
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	home, _ := os.UserHomeDir()
	return installationSourceForPath(path, os.Getenv("GOBIN"), os.Getenv("GOPATH"), home)
}

func installationSourceForPath(path, goBin, goPath, home string) string {
	cleanPath := filepath.Clean(path)
	portablePath := filepath.ToSlash(cleanPath)

	// Nix first: store paths can embed substrings that look like Homebrew.
	if strings.HasPrefix(portablePath, "/nix/store/") {
		return installationSourceNix
	}
	// Homebrew layouts embed "/opt/crit/" (e.g. /opt/homebrew/opt/crit/) or
	// "/Cellar/crit/". Exclude a bare /opt/crit/... prefix so a manual FHS
	// install is not mislabeled as Homebrew.
	if strings.Contains(portablePath, "/Cellar/crit/") ||
		(strings.Contains(portablePath, "/opt/crit/") && !strings.HasPrefix(portablePath, "/opt/crit/")) {
		return installationSourceHomebrew
	}

	goBins := []string{goBin}
	if goPath != "" {
		for _, root := range filepath.SplitList(goPath) {
			goBins = append(goBins, filepath.Join(root, "bin"))
		}
	}
	if home != "" {
		goBins = append(goBins, filepath.Join(home, "go", "bin"))
	}
	for _, bin := range goBins {
		if bin != "" && filepath.Dir(cleanPath) == filepath.Clean(bin) {
			return installationSourceGo
		}
	}
	return installationSourceUnknown
}
