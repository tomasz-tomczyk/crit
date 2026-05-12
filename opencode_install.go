package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// opencodePluginEntry returns the relative path written into the opencode
// config's `plugin` array. opencode resolves relative paths against the config
// file's directory, so project and global installs need different entries:
//
//	project: ./opencode.jsonc + ./.opencode/plugins/crit.ts → "./.opencode/plugins/crit.ts"
//	global:  ~/.config/opencode/opencode.jsonc + ~/.config/opencode/plugins/crit.ts → "./plugins/crit.ts"
//
// opencode auto-loads any .ts under its plugin dir, so the registration is
// informational — but if we write it at all, it has to point at the right file.
func opencodePluginEntry(global bool) string {
	if global {
		return "./plugins/crit.ts"
	}
	return "./.opencode/plugins/crit.ts"
}

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
// To avoid clobbering hand-tuned configs (json.Marshal alphabetizes keys and
// loses formatting), we only auto-write when the existing file is empty or
// contains only the `plugin` key. Any other top-level keys → we print the
// exact line the user needs to add and leave the file alone. Same policy for
// JSONC files with comments, which encoding/json can't round-trip safely.
func installOpencodePluginEntry(path, entry string, force bool) error {
	root := map[string]interface{}{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if looksLikeJSONC(data) {
			printManualPluginInstruction(path, "contains comments", entry)
			return nil
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s contains invalid JSON: %w", path, err)
		}
		if hasUnrelatedKeys(root) {
			printManualPluginInstruction(path, "has other config keys we won't rewrite", entry)
			return nil
		}
		// If `plugin` exists but isn't an array (string shorthand, null, object),
		// we don't know how to safely append — bail rather than clobber.
		if raw, ok := root["plugin"]; ok && raw != nil {
			if _, ok := raw.([]interface{}); !ok {
				printManualPluginInstruction(path, "\"plugin\" key is not an array", entry)
				return nil
			}
		}
	case errors.Is(readErr, os.ErrNotExist):
		// new file
	default:
		return readErr
	}

	plugins, _ := root["plugin"].([]interface{})
	if pluginEntryPresent(plugins, entry) {
		if !force {
			fmt.Printf("  Skipped:   %s (plugin already registered)\n", path)
			return nil
		}
	} else {
		plugins = append(plugins, entry)
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

// hasUnrelatedKeys reports whether the parsed root contains anything other
// than the `plugin` key. Re-marshaling a map[string]interface{} reorders keys
// and strips formatting, so we treat any extra content as a signal to leave
// the file alone and ask the user to register the plugin manually.
func hasUnrelatedKeys(root map[string]interface{}) bool {
	for k := range root {
		if k != "plugin" {
			return true
		}
	}
	return false
}

// printManualPluginInstruction tells the user the exact line to paste into
// their opencode config when we decline to rewrite the file.
func printManualPluginInstruction(path, reason, entry string) {
	fmt.Printf("  Skipped:   %s (%s)\n", path, reason)
	fmt.Printf("             Add the plugin manually — inside the top-level object, set:\n")
	fmt.Printf("               \"plugin\": [%q]\n", entry)
	fmt.Printf("             (or append %q to the existing \"plugin\" array)\n", entry)
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
