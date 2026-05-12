package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// opencodePluginEntry is the relative path entry written into the opencode
// config's `plugin` array. opencode auto-loads anything in `.opencode/plugins/`
// regardless of this entry — the registration is belt-and-suspenders so that
// users who scan their config can see crit's plugin is wired up.
const opencodePluginEntry = "./.opencode/plugins/crit.ts"

// opencodeConfigPath returns the opencode config file to edit. Project installs
// target `./opencode.jsonc`; global installs target `~/.config/opencode/opencode.jsonc`.
// If a `.json` variant exists in the same directory, that path is returned instead
// so we don't create a parallel `.jsonc` next to it.
func opencodeConfigPath(global bool, home string) string {
	var dir string
	if global {
		dir = filepath.Join(home, ".config", "opencode")
	} else {
		dir = "."
	}
	jsonc := filepath.Join(dir, "opencode.jsonc")
	plain := filepath.Join(dir, "opencode.json")
	if _, err := os.Stat(plain); err == nil {
		return plain
	}
	return jsonc
}

// installOpencodePluginEntry adds crit's plugin path to the `plugin` array in
// the user's opencode config, creating the file if missing. Idempotent: if the
// entry already exists the file is left untouched.
//
// Limitation: opencode.jsonc may contain comments and trailing commas. We
// parse with encoding/json which is strict — if the existing file has comments
// or other JSONC-only syntax this returns an error and we don't touch it. Users
// are advised in that case to add `"./.opencode/plugins/crit.ts"` to the
// `plugin` array by hand.
func installOpencodePluginEntry(path string, force bool) error {
	root := map[string]interface{}{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if looksLikeJSONC(data) {
			fmt.Printf("  Skipped:   %s (contains comments — add %q to the \"plugin\" array manually)\n", path, opencodePluginEntry)
			return nil
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s contains invalid JSON: %w", path, err)
		}
	case errors.Is(readErr, os.ErrNotExist):
		// new file
	default:
		return readErr
	}

	plugins, _ := root["plugin"].([]interface{})
	if pluginEntryPresent(plugins, opencodePluginEntry) {
		if !force {
			fmt.Printf("  Skipped:   %s (plugin already registered)\n", path)
			return nil
		}
	} else {
		plugins = append(plugins, opencodePluginEntry)
	}
	root["plugin"] = plugins

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := atomicWriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Printf("  Installed: %s\n", path)
	return nil
}

// pluginEntryPresent reports whether `entry` (or its `[name, options]` tuple
// form) already exists in the opencode plugin array.
func pluginEntryPresent(plugins []interface{}, entry string) bool {
	for _, p := range plugins {
		switch v := p.(type) {
		case string:
			if v == entry {
				return true
			}
		case []interface{}:
			if len(v) > 0 {
				if name, ok := v[0].(string); ok && name == entry {
					return true
				}
			}
		}
	}
	return false
}

// looksLikeJSONC returns true if the file content contains line or block
// comments. encoding/json would reject these, so we treat such files as
// hands-off rather than silently stripping the comments.
func looksLikeJSONC(data []byte) bool {
	// Strip strings before scanning so `"// not a comment"` doesn't trigger.
	stripped := stripJSONStrings(data)
	return strings.Contains(stripped, "//") || strings.Contains(stripped, "/*")
}

// stripJSONStrings replaces every JSON string literal in data with empty
// quotes so a subsequent comment scan won't false-positive on `//` or `/*`
// that lives inside a string. Handles escape sequences via the standard
// JSON rule that a `\` escapes the next byte.
func stripJSONStrings(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	inString := false
	escape := false
	for _, c := range data {
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
				b.WriteByte('"')
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte('"')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
