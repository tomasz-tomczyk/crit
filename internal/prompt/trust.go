package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// ContentHash fingerprints project prompt config and referenced files.
func ContentHash(projectPrompts map[string]string, projectDir string) string {
	h := sha256.New()
	keys := make([]string, 0, len(projectPrompts))
	for k := range projectPrompts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := projectPrompts[k]
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
	for _, path := range ListDiscoveredProjectPromptFiles(projectDir) {
		h.Write([]byte("discovered:"))
		h.Write([]byte(filepath.Base(path)))
		h.Write([]byte{0})
		if data, err := os.ReadFile(path); err == nil {
			h.Write(data)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
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

// EvaluateTrust decides if project prompts are trusted for finish.
func EvaluateTrust(projectDir string, projectPrompts map[string]string) (TrustState, error) {
	st := TrustState{
		ContentHash: ContentHash(projectPrompts, projectDir),
		Sources:     ListProjectPromptSources(projectPrompts, projectDir),
	}
	discovered := ListDiscoveredProjectPromptFiles(projectDir)
	for _, path := range discovered {
		label := "project:" + filepath.ToSlash(filepath.Join(promptsSubdir, filepath.Base(path)))
		st.Sources = append(st.Sources, label)
	}
	if len(projectPrompts) == 0 && len(discovered) == 0 {
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
