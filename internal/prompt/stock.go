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
