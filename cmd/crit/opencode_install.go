package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

func opencodePluginPackagePath(global bool, home string) string {
	if global {
		return filepath.Join(home, ".config", "opencode", "package.json")
	}
	return filepath.Join(".opencode", "package.json")
}

// installOpencodePluginDependency declares the V2 plugin SDK in OpenCode's
// dependency manifest. OpenCode installs dependencies declared there before
// loading local plugins.
func installOpencodePluginDependency(path, version string, force bool) error {
	if version == "" {
		return errors.New("could not determine the OpenCode v2 version for @opencode/plugin")
	}
	root := map[string]interface{}{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s contains invalid JSON: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dependencies, ok := root["dependencies"].(map[string]interface{})
	if !ok {
		if _, exists := root["dependencies"]; exists {
			return fmt.Errorf("%s has a dependencies field that is not an object", path)
		}
		dependencies = map[string]interface{}{}
	}
	if current, ok := dependencies["@opencode/plugin"].(string); ok {
		if current == version {
			if opencodePluginDependencyInstalled(filepath.Dir(path), version) {
				fmt.Printf("  Skipped:   %s (@opencode/plugin %s is ready)\n", path, version)
				return nil
			}
			return installOpencodeDependencies(filepath.Dir(path))
		}
		if !force {
			return fmt.Errorf("%s declares @opencode/plugin %q, but OpenCode v2 %s requires a matching SDK; rerun with --force to update it", path, current, version)
		}
	}
	dependencies["@opencode/plugin"] = version
	root["dependencies"] = dependencies
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := atomicWriteFile(path, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Printf("  Installed: %s (@opencode/plugin %s)\n", path, version)
	return installOpencodeDependencies(filepath.Dir(path))
}

func installOpencodeDependencies(dir string) error {
	manager, args := "", []string(nil)
	if _, err := exec.LookPath("bun"); err == nil {
		manager, args = "bun", []string{"install"}
	} else if _, err := exec.LookPath("npm"); err == nil {
		manager, args = "npm", []string{"install", "--no-audit", "--no-fund"}
	} else {
		return errors.New("OpenCode v2 plugin dependencies require bun or npm on PATH")
	}
	fmt.Printf("  Installing: OpenCode v2 plugin dependencies with %s\n", manager)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, manager, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s install failed in %s: %w\n%s", manager, dir, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func opencodePluginDependencyInstalled(configDir, version string) bool {
	path := filepath.Join(configDir, "node_modules", "@opencode", "plugin", "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var installed struct {
		Version string `json:"version"`
	}
	return json.Unmarshal(data, &installed) == nil && installed.Version == version
}

func checkOpencodePluginDependency(projectDir, homeDir string) []staleFile {
	major, version := opencodeVersionInfo()
	return checkOpencodePluginDependencyForVersion(projectDir, homeDir, major, version)
}

func checkOpencodePluginDependencyForVersion(projectDir, homeDir string, major int, version string) []staleFile {
	if major != 2 || version == "" {
		return nil
	}
	type pluginRoot struct {
		pluginPath  string
		packagePath string
		location    string
	}
	roots := []pluginRoot{
		{
			pluginPath:  filepath.Join(projectDir, ".opencode", "plugins", "crit", "index.ts"),
			packagePath: filepath.Join(projectDir, ".opencode", "package.json"),
			location:    locationProject,
		},
		{
			pluginPath:  filepath.Join(homeDir, ".config", "opencode", "plugins", "crit", "index.ts"),
			packagePath: filepath.Join(homeDir, ".config", "opencode", "package.json"),
			location:    locationHome,
		},
	}
	var results []staleFile
	for _, root := range roots {
		if _, err := os.Stat(root.pluginPath); err != nil {
			continue
		}
		data, err := os.ReadFile(root.packagePath)
		var config struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if err == nil {
			_ = json.Unmarshal(data, &config)
		}
		if err == nil && opencodePluginDependencyInstalled(filepath.Dir(root.packagePath), version) && config.Dependencies["@opencode/plugin"] == version {
			continue
		}
		results = append(results, staleFile{
			agent: "opencode", file: filepath.Base(root.packagePath), dest: root.packagePath,
			location: root.location,
		})
	}
	return results
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
		data = stripTrailingCommas(data)
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

// removeOpencodePluginEntry removes a V1 plugin registration when migrating
// to V2. It follows the same conservative rewrite rules as installation.
func removeOpencodePluginEntry(path, entry string) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not inspect %s: %v\n", path, err)
		return
	}
	if looksLikeJSONC(data) {
		fmt.Printf("  Note:      remove the V1 plugin registration %q from %s; V2 discovers the Crit plugin automatically\n", entry, path)
		return
	}
	root := map[string]interface{}{}
	if err := json.Unmarshal(stripTrailingCommas(data), &root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not remove the V1 plugin registration from %s: %v\n", path, err)
		return
	}
	plugins, ok := root["plugin"].([]interface{})
	if !ok {
		return
	}
	if hasUnrelatedKeys(root) {
		fmt.Printf("  Note:      remove the V1 plugin registration %q from %s; V2 discovers the Crit plugin automatically\n", entry, path)
		return
	}
	filtered := make([]interface{}, 0, len(plugins))
	for _, plugin := range plugins {
		if !pluginEntryPresent([]interface{}{plugin}, entry) {
			filtered = append(filtered, plugin)
		}
	}
	if len(filtered) == len(plugins) {
		return
	}
	if len(filtered) > 0 {
		root["plugin"] = filtered
	} else {
		delete(root, "plugin")
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not encode %s: %v\n", path, err)
		return
	}
	if err := atomicWriteFile(path, append(out, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update %s: %v\n", path, err)
	}
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

// stripTrailingCommas removes commas that appear immediately before a closing
// `}` or `]` outside of JSON string literals. This lets us tolerate configs
// that are otherwise plain JSON but contain a trailing comma, which many
// editors and JSONC-style configs allow but encoding/json rejects.
func stripTrailingCommas(data []byte) []byte {
	var out []byte
	inString := false
	escape := false
	lastComma := -1 // index in out of the most recent unconfirmed comma

	for _, c := range data {
		if inString {
			if escape {
				escape = false
				out = append(out, c)
				continue
			}
			if c == '\\' {
				escape = true
				out = append(out, c)
				continue
			}
			out = append(out, c)
			if c == '"' {
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
			lastComma = -1 // a value/key follows, so the comma was not trailing
			out = append(out, c)
		case ',':
			lastComma = len(out)
			out = append(out, c)
		case ' ', '\t', '\n', '\r':
			out = append(out, c)
		case '}', ']':
			if lastComma >= 0 {
				out = out[:lastComma]
				lastComma = -1
			}
			out = append(out, c)
		default:
			lastComma = -1 // any other token means the comma was not trailing
			out = append(out, c)
		}
	}

	return out
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
