package webassets

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

// shippedLanguages returns every grammar name (ids and aliases) embedded in
// pierre/, written there by scripts/build-pierre.mjs.
var shippedLanguages = sync.OnceValues(func() ([]string, error) {
	data, err := FS.ReadFile("pierre/languages.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded language list: %w", err)
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("parsing embedded language list: %w", err)
	}
	return names, nil
})

// CompileLanguages validates the `languages` config against the grammars
// this binary ships.
func CompileLanguages(languages map[string]string) (config.LanguageRules, error) {
	if len(languages) == 0 {
		return config.LanguageRules{}, nil
	}
	known, err := shippedLanguages()
	if err != nil {
		return config.LanguageRules{}, err
	}
	return config.CompileLanguages(languages, known)
}
