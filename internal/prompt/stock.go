package prompt

import integrationassets "github.com/tomasz-tomczyk/crit/integrations"

// LoadStockTemplate reads a built-in finish template from embedded integrations/prompts/.
func LoadStockTemplate(hook, mode string) (text, source string, ok bool) {
	for _, name := range PromptFilenames(hook, mode) {
		path := "integrations/prompts/" + name
		data, err := integrationassets.FS.ReadFile(path)
		if err != nil {
			continue
		}
		return string(data), "stock:" + name, true
	}
	return "", "", false
}

// LoadStockTemplateSpecific reads one built-in prompt template without
// falling back from a mode-specific hook to the generic hook.
func LoadStockTemplateSpecific(hook, mode string) (text, source string, ok bool) {
	key := hook
	if mode != "" {
		key, _ = ResolveHookKey(hook, mode)
	}
	name := hookKeyToFilename(key)
	path := "integrations/prompts/" + name
	data, err := integrationassets.FS.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	return string(data), "stock:" + name, true
}
