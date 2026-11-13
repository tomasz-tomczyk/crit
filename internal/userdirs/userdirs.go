// Package userdirs resolves crit's user-level directories: the global config
// file, the global prompt and hook directories, the custom stylesheet, and the
// state root that holds sessions, reviews, plans, stats and exports.
//
// crit follows the XDG Base Directory spec while keeping the legacy layout
// (~/.crit.config.json and ~/.crit/) working:
//
//   - config (config.json, prompts/, hooks/, custom.css):
//     $XDG_CONFIG_HOME/crit, defaulting to ~/.config/crit
//   - state (sessions/, reviews/, plans/, plan-sessions.json, stats.json,
//     exports/): $XDG_STATE_HOME/crit, defaulting to ~/.local/state/crit
//
// Each path is resolved on its own: the XDG location is used when it exists,
// otherwise the legacy location when it exists. A path that exists in neither
// place (a file crit is about to create) goes to the legacy location on an
// existing install (~/.crit or ~/.crit.config.json present) and to the XDG
// location otherwise. Existing installs therefore keep the legacy layout until
// the user moves things over, and a fresh install never creates ~/.crit. XDG variables that are unset, empty or relative are ignored,
// as the spec requires. On Windows the XDG defaults do not apply, so without
// the variables crit keeps the legacy layout there.
package userdirs

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "crit"

// goos is runtime.GOOS in production; tests replace it to cover Windows.
var goos = runtime.GOOS

// LegacyDir is the pre-XDG crit directory, ~/.crit.
func LegacyDir(home string) string {
	return filepath.Join(home, ".crit")
}

// ConfigDir is the XDG config directory for crit, or "" when only the legacy
// layout applies (Windows without XDG_CONFIG_HOME).
func ConfigDir(home string) string {
	return xdgDir(home, "XDG_CONFIG_HOME", ".config")
}

// StateBaseDir is the XDG state directory for crit, or "" when only the legacy
// layout applies (Windows without XDG_STATE_HOME).
func StateBaseDir(home string) string {
	return xdgDir(home, "XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// ConfigFile is the global config file: $XDG_CONFIG_HOME/crit/config.json or
// the legacy ~/.crit.config.json.
func ConfigFile(home string) string {
	return pick(home, join(ConfigDir(home), "config.json"), legacyConfigFile(home))
}

// PromptsDir is the global prompt template directory:
// $XDG_CONFIG_HOME/crit/prompts or the legacy ~/.crit/prompts.
func PromptsDir(home string) string {
	return pick(home, join(ConfigDir(home), "prompts"), filepath.Join(LegacyDir(home), "prompts"))
}

// HooksDir is the global hook script directory: $XDG_CONFIG_HOME/crit/hooks or
// the legacy ~/.crit/hooks.
func HooksDir(home string) string {
	return pick(home, join(ConfigDir(home), "hooks"), filepath.Join(LegacyDir(home), "hooks"))
}

// CustomCSS is the user stylesheet: $XDG_CONFIG_HOME/crit/custom.css or the
// legacy ~/.crit/custom.css.
func CustomCSS(home string) string {
	return pick(home, join(ConfigDir(home), "custom.css"), filepath.Join(LegacyDir(home), "custom.css"))
}

// StateDir is the root for crit's machine-local data (sessions, reviews,
// plans, stats, exports): $XDG_STATE_HOME/crit or the legacy ~/.crit. The
// `output` config key and --output flag still override where reviews go.
func StateDir(home string) string {
	return pick(home, StateBaseDir(home), LegacyDir(home))
}

func xdgDir(home, envVar, defaultRel string) string {
	if v := os.Getenv(envVar); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, appName)
	}
	if goos == "windows" {
		return ""
	}
	return filepath.Join(home, defaultRel, appName)
}

func join(dir, name string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// IsLegacyInstall reports whether home holds a pre-XDG crit install: a ~/.crit
// directory or a ~/.crit.config.json file.
func IsLegacyInstall(home string) bool {
	return exists(LegacyDir(home)) || exists(legacyConfigFile(home))
}

func legacyConfigFile(home string) string {
	return filepath.Join(home, ".crit.config.json")
}

// pick returns xdg when it exists, else legacy when it exists. When neither
// exists it returns legacy on a legacy install and xdg otherwise. An empty xdg
// means only the legacy layout applies.
func pick(home, xdg, legacy string) string {
	if xdg == "" {
		return legacy
	}
	if exists(xdg) {
		return xdg
	}
	if exists(legacy) || IsLegacyInstall(home) {
		return legacy
	}
	return xdg
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
