package webassets

import (
	"slices"
	"testing"
)

func TestShippedLanguages(t *testing.T) {
	names, err := shippedLanguages()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"yaml", "yml", "glimmer-ts", "typescript", "ts", "markdown"} {
		if !slices.Contains(names, want) {
			t.Errorf("embedded language list is missing %q", want)
		}
	}
	if _, err := CompileLanguages(map[string]string{"*.gts": "glimmer-ts", "*.qmd": "markdown"}); err != nil {
		t.Errorf("CompileLanguages: %v", err)
	}
	if _, err := CompileLanguages(map[string]string{"*.x": "not-a-grammar"}); err == nil {
		t.Error("CompileLanguages accepted an unknown grammar")
	}
}
