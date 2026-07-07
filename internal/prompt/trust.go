package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

const (
	TrustUntilChange = "until_change"
	TrustAlways      = "always"
	TrustDefaults    = "defaults"
)

// TrustEntry is persisted in global config trusted_project_prompts.
type TrustEntry struct {
	Mode        string `json:"mode"`
	ContentHash string `json:"content_hash,omitempty"`
}

// RepoRootHash returns a stable key for trusted_project_prompts.
func RepoRootHash(projectDir string) string {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	sum := sha256.Sum256([]byte(abs))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ContentHash fingerprints project prompt AND hook config plus referenced
// files, so changing either prompts or hooks invalidates a "trust until
// change" decision. Hooks are included because project hooks run arbitrary code
// and must be gated by the same trust flow as project prompts.
func ContentHash(projectPrompts, projectHooks map[string]string, projectDir string) string {
	h := sha256.New()
	hashConfigMap(h, "prompt:", projectPrompts, projectDir)
	for _, path := range ListDiscoveredProjectPromptFiles(projectDir) {
		h.Write([]byte("discovered-prompt:"))
		h.Write([]byte(filepath.Base(path)))
		h.Write([]byte{0})
		if data, err := os.ReadFile(path); err == nil {
			h.Write(data)
		}
	}
	hashConfigMap(h, "hook:", projectHooks, projectDir)
	for _, path := range listDiscoveredProjectHookFiles(projectDir) {
		h.Write([]byte("discovered-hook:"))
		h.Write([]byte(filepath.Base(path)))
		h.Write([]byte{0})
		if data, err := os.ReadFile(path); err == nil {
			h.Write(data)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// hashConfigMap writes a sorted key/value section into the hash, and for file:
// values it folds the referenced file's contents (resolved relative to
// projectDir). inline: values contribute only their key+value (no file).
func hashConfigMap(h hash.Hash, section string, m map[string]string, projectDir string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		h.Write([]byte(section))
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(v))
		h.Write([]byte{0})
		if strings.HasPrefix(v, prefixFile) {
			path := strings.TrimPrefix(v, prefixFile)
			if !filepath.IsAbs(path) {
				path = filepath.Join(projectDir, path)
			}
			if data, err := os.ReadFile(path); err == nil {
				h.Write(data)
			}
		}
	}
}

// LoadTrustedProjectPrompts reads trusted_project_prompts from global config.
func LoadTrustedProjectPrompts() (map[string]TrustEntry, error) {
	path := config.GlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]TrustEntry{}, nil
		}
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	entry, ok := raw["trusted_project_prompts"]
	if !ok {
		return map[string]TrustEntry{}, nil
	}
	var out map[string]TrustEntry
	if err := json.Unmarshal(entry, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]TrustEntry{}, nil
	}
	return out, nil
}

// SaveTrustChoice persists a trust decision for a project.
func SaveTrustChoice(projectDir string, mode string, contentHash string) error {
	key := RepoRootHash(projectDir)
	return config.SaveGlobalConfig(func(m map[string]json.RawMessage) error {
		trusted := map[string]TrustEntry{}
		if raw, ok := m["trusted_project_prompts"]; ok {
			_ = json.Unmarshal(raw, &trusted)
		}
		if trusted == nil {
			trusted = map[string]TrustEntry{}
		}
		entry := TrustEntry{Mode: mode}
		if mode == TrustUntilChange {
			entry.ContentHash = contentHash
		}
		trusted[key] = entry
		b, err := json.Marshal(trusted)
		if err != nil {
			return err
		}
		m["trusted_project_prompts"] = b
		return nil
	})
}

// TrustState describes whether project prompts may be used.
type TrustState struct {
	HasProjectPrompts bool
	Untrusted         bool
	UseProject        bool // false when user chose defaults
	ContentHash       string
	Sources           []string
}

// EvaluateTrust decides if project prompts AND hooks are trusted for finish.
// The trust gate now covers both: project hooks run arbitrary code, so a
// checked-in .crit/hooks/*.sh or `hooks` config map triggers Finish blocking
// exactly like project prompt config does.
func EvaluateTrust(projectDir string, projectPrompts, projectHooks map[string]string) (TrustState, error) {
	st := TrustState{
		ContentHash: ContentHash(projectPrompts, projectHooks, projectDir),
		Sources:     ListProjectPromptSources(projectPrompts, projectDir),
	}
	st.Sources = append(st.Sources, listProjectHookSources(projectHooks, projectDir)...)

	discoveredPrompts := ListDiscoveredProjectPromptFiles(projectDir)
	for _, path := range discoveredPrompts {
		label := "project:" + filepath.ToSlash(filepath.Join(promptsSubdir, filepath.Base(path)))
		st.Sources = append(st.Sources, label)
	}
	discoveredHooks := listDiscoveredProjectHookFiles(projectDir)
	for _, path := range discoveredHooks {
		label := "project:" + filepath.ToSlash(filepath.Join(hooksSubdir, filepath.Base(path)))
		st.Sources = append(st.Sources, label)
	}

	if len(projectPrompts) == 0 && len(projectHooks) == 0 && len(discoveredPrompts) == 0 && len(discoveredHooks) == 0 {
		st.UseProject = false
		return st, nil
	}
	st.HasProjectPrompts = true

	trusted, err := LoadTrustedProjectPrompts()
	if err != nil {
		return st, fmt.Errorf("loading trust config: %w", err)
	}
	entry, ok := trusted[RepoRootHash(projectDir)]
	if !ok {
		st.Untrusted = true
		return st, nil
	}
	switch entry.Mode {
	case TrustDefaults:
		st.UseProject = false
		return st, nil
	case TrustAlways:
		st.UseProject = true
		return st, nil
	case TrustUntilChange:
		if entry.ContentHash != st.ContentHash {
			st.Untrusted = true
			return st, nil
		}
		st.UseProject = true
		return st, nil
	default:
		st.Untrusted = true
		return st, nil
	}
}

// listDiscoveredProjectHookFiles returns on_finish_*.sh paths under
// project/.crit/hooks/. Mirrors ListDiscoveredProjectPromptFiles for the hook
// half of the trust gate. Defined here (not in internal/hooks) to avoid an
// import cycle: internal/hooks already imports internal/prompt for resolution.
func listDiscoveredProjectHookFiles(projectDir string) []string {
	dir := filepath.Join(projectDir, hooksSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		if !strings.HasPrefix(e.Name(), "on_finish_") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// listProjectHookSources returns human-readable source paths for project hook
// config. Mirrors ListProjectPromptSources for the hook half of the trust gate.
func listProjectHookSources(projectHooks map[string]string, projectDir string) []string {
	if len(projectHooks) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	configPath := filepath.Join(projectDir, ".crit.config.json")
	if _, err := os.Stat(configPath); err == nil {
		out = append(out, "project:.crit.config.json")
		seen["project:.crit.config.json"] = struct{}{}
	}
	for _, v := range projectHooks {
		if !strings.HasPrefix(v, prefixFile) {
			continue
		}
		rel := strings.TrimPrefix(v, prefixFile)
		label := "project:" + filepath.ToSlash(rel)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}
