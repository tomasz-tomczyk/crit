package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// aiderInstallPaths describes where the aider integration files live for
// either project or global installs. Paths are absolute on disk except
// readEntry which is the literal value to put under "read:" in the YAML
// (aider expands "~" itself, so we keep that form for global mode).
type aiderInstallPaths struct {
	conventionsDest string // absolute path to write CONVENTIONS.md
	confPath        string // absolute path to .aider.conf.yml
	readEntry       string // value to add to the read: list
}

// aiderPaths returns the install paths for aider given a cwd and home dir.
// In global mode (cwd == home) we write ~/.crit-conventions.md and update
// ~/.aider.conf.yml. In project mode we write .crit/aider-conventions.md
// and update ./.aider.conf.yml.
func aiderPaths(cwd, home string) aiderInstallPaths {
	if isGlobalInstall(cwd, home) {
		return aiderInstallPaths{
			conventionsDest: filepath.Join(home, ".crit-conventions.md"),
			confPath:        filepath.Join(home, ".aider.conf.yml"),
			readEntry:       "~/.crit-conventions.md",
		}
	}
	return aiderInstallPaths{
		conventionsDest: filepath.Join(cwd, ".crit", "aider-conventions.md"),
		confPath:        filepath.Join(cwd, ".aider.conf.yml"),
		readEntry:       ".crit/aider-conventions.md",
	}
}

// installAider implements `crit install aider`. Thin wrapper that resolves
// cwd/home and delegates to installAiderAt for testability.
func installAider(force bool) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	if err := installAiderAt(cwd, home, force); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// installAiderAt is the testable core of installAider. It writes the embedded
// CONVENTIONS.md and merges the read entry into .aider.conf.yml. Returns a
// formatted error suitable for printing to stderr; the caller decides whether
// to exit.
func installAiderAt(cwd, home string, force bool) error {
	paths := aiderPaths(cwd, home)

	data, err := integrationsFS.ReadFile("integrations/aider/CONVENTIONS.md")
	if err != nil {
		return fmt.Errorf("reading embedded aider conventions: %w", err)
	}

	if !force {
		if _, statErr := os.Stat(paths.conventionsDest); statErr == nil {
			fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", paths.conventionsDest)
		} else {
			if err := writeFileMkdirAtomic(paths.conventionsDest, data); err != nil {
				return fmt.Errorf("writing %s: %w", paths.conventionsDest, err)
			}
			fmt.Printf("  Installed: %s\n", paths.conventionsDest)
		}
	} else {
		if err := writeFileMkdirAtomic(paths.conventionsDest, data); err != nil {
			return fmt.Errorf("writing %s: %w", paths.conventionsDest, err)
		}
		fmt.Printf("  Installed: %s\n", paths.conventionsDest)
	}

	// Always merge the conf — merging is idempotent and the conventions
	// file alone is useless without the conf entry.
	if err := mergeAiderConfFile(paths.confPath, paths.readEntry); err != nil {
		return fmt.Errorf("updating %s: %w", paths.confPath, err)
	}
	fmt.Printf("  Updated:   %s (added %s under read:)\n", paths.confPath, paths.readEntry)
	fmt.Println("  Aider will load the crit conventions on next start")
	fmt.Println()
	return nil
}

// writeFileMkdir writes data to path, creating parent directories as needed.
// Use writeFileMkdirAtomic for files where a partial write would be harmful.
func writeFileMkdir(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeFileMkdirAtomic writes data to path via a same-directory tempfile
// followed by rename. On POSIX rename is atomic, so a crash mid-write cannot
// leave a truncated file at path. Parent dirs are created as needed.
func writeFileMkdirAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, filepath.Base(path)+".tmp."+strconv.Itoa(os.Getpid()))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// multiDocYAMLRe matches a YAML document separator at the start of a line,
// optionally followed by whitespace or end-of-line. The leading "(?m)" makes
// "^" anchor to line starts.
var multiDocYAMLRe = regexp.MustCompile(`(?m)^---\s*$`)

// utf8BOM is the byte-order mark sometimes prepended to UTF-8 files by
// Windows editors. yaml.Unmarshal does not strip it.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// mergeAiderConfFile reads the YAML file at path, ensures readEntry is
// present in the top-level "read:" list (creating the file or list as
// needed), and writes the result back atomically. Other top-level keys are
// preserved.
//
// Implementation note: we round-trip through yaml.Node so existing keys,
// ordering, and (where yaml.v3 supports it) comments are retained. If a
// legacy file uses a scalar "read:" value (a single string), we promote it
// to a sequence containing that string plus our entry. Idempotent.
func mergeAiderConfFile(path, readEntry string) error {
	var existing []byte
	if b, err := os.ReadFile(path); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	merged, err := mergeAiderConfYAML(existing, readEntry)
	if err != nil {
		return err
	}

	return writeFileMkdirAtomic(path, merged)
}

// mergeAiderConfYAML returns the YAML bytes resulting from merging readEntry
// into the read: list of the given YAML document. existing may be empty/nil,
// in which case a new minimal document is produced. Pure function — no I/O.
func mergeAiderConfYAML(existing []byte, readEntry string) ([]byte, error) {
	// Strip a leading UTF-8 BOM if present — yaml.Unmarshal does not.
	existing = bytes.TrimPrefix(existing, utf8BOM)

	// Reject multi-document YAML before we silently destroy the trailing
	// docs by re-marshaling only the first.
	if isMultiDocYAML(existing) {
		return nil, fmt.Errorf("multi-document YAML in .aider.conf.yml is not supported by crit install — please consolidate or skip")
	}

	// Empty or whitespace-only file: produce a fresh document.
	if len(bytes.TrimSpace(existing)) == 0 {
		out := map[string]any{"read": []string{readEntry}}
		return yaml.Marshal(out)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(existing, &root); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	// yaml.Unmarshal into a Node yields a DocumentNode wrapping the real
	// value. If the doc was empty, root.Kind == 0.
	if root.Kind == 0 {
		out := map[string]any{"read": []string{readEntry}}
		return yaml.Marshal(out)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("unexpected yaml structure (kind %d)", root.Kind)
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("aider conf root must be a mapping, got kind %d", doc.Kind)
	}

	if err := upsertReadEntry(doc, readEntry); err != nil {
		return nil, err
	}

	return yaml.Marshal(&root)
}

// isMultiDocYAML reports whether b contains a YAML document separator
// ("---" on its own line) anywhere after the first byte. A leading "---" on
// the very first line is a single-doc start marker and is not multi-doc.
func isMultiDocYAML(b []byte) bool {
	locs := multiDocYAMLRe.FindAllIndex(b, -1)
	for _, loc := range locs {
		if loc[0] != 0 {
			return true
		}
	}
	return false
}

// upsertReadEntry adds readEntry to the "read:" key of mapping. If "read:"
// is absent, it's appended as a sequence. If it's a scalar (single string),
// it's promoted to a sequence containing the scalar plus the new entry. If
// it's already a sequence, the entry is appended unless already present.
func upsertReadEntry(mapping *yaml.Node, readEntry string) error {
	// MappingNode content is [key0, val0, key1, val1, ...].
	for i := 0; i < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Value != "read" {
			continue
		}
		v := mapping.Content[i+1]
		switch v.Kind {
		case yaml.SequenceNode:
			for _, item := range v.Content {
				if item.Kind == yaml.ScalarNode && item.Value == readEntry {
					return nil // already present, nothing to do
				}
			}
			v.Content = append(v.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: readEntry})
			return nil
		case yaml.ScalarNode:
			if v.Value == readEntry {
				return nil
			}
			old := v.Value
			// Promote scalar to sequence.
			*v = yaml.Node{
				Kind: yaml.SequenceNode,
				Tag:  "!!seq",
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: old},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: readEntry},
				},
			}
			return nil
		default:
			return fmt.Errorf("aider conf 'read' has unsupported yaml kind %d", v.Kind)
		}
	}

	// "read:" not present — append it.
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "read"},
		&yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: readEntry},
			},
		},
	)
	return nil
}
