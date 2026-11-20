package server

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
	if _, err := compileLanguages(map[string]string{"*.gts": "glimmer-ts", "*.qmd": "markdown"}); err != nil {
		t.Errorf("compileLanguages: %v", err)
	}
	if _, err := compileLanguages(map[string]string{"*.x": "not-a-grammar"}); err == nil {
		t.Error("compileLanguages accepted an unknown grammar")
	}
}
