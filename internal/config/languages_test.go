package config

import (
	"strings"
	"testing"
)

func TestCompileLanguages(t *testing.T) {
	known := []string{"yaml", "yml", "glimmer-js", "glimmer-ts", "c", "toml"}
	tests := []struct {
		name      string
		languages map[string]string
		wantErr   []string
	}{
		{name: "empty", languages: nil},
		{name: "known grammar, markdown, md, text, its aliases and auto", languages: map[string]string{
			"*.conf": "YAML", "*.qmd": "markdown", "*.Rmd": "md", "*.log": "text",
			"*.out": "plaintext", "*.txt": "txt", "*.raw": "plain", "*.cfg": "auto",
		}},
		{name: "unknown names reported together with suggestions", languages: map[string]string{
			"*.gts": "glimmer", "*.conf": "yaml2", "*.x": "zzz",
		}, wantErr: []string{
			`"*.gts": unknown language "glimmer" (did you mean glimmer-js, glimmer-ts?)`,
			`"*.conf": unknown language "yaml2" (did you mean yaml?)`,
			`"*.x": unknown language "zzz"` + "\n",
		}},
		{name: "empty pattern and value", languages: map[string]string{"": "yaml", "*.y": " "},
			wantErr: []string{`empty pattern for "yaml"`, `"*.y": empty language`}},
		{name: "malformed glob", languages: map[string]string{"[abc": "yaml", "conf/[x": "toml"},
			wantErr: []string{`"[abc": invalid pattern`, `"conf/[x": invalid pattern`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileLanguages(tc.languages, known)
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error() + "\n"
			for _, want := range tc.wantErr {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q missing %q", msg, want)
				}
			}
		})
	}
}

func TestLanguageRules_MostSpecificPatternWins(t *testing.T) {
	rules, err := CompileLanguages(map[string]string{
		"*.conf":        "toml",
		"deploy/*.conf": "yaml",
		"docs/":         "markdown",
		"nginx.conf":    "nginx",
		"*/*.conf":      "ini",
		"*.qmd":         "markdown",
		"*.md":          "plaintext",
		"vendor/":       "auto",
	}, []string{"toml", "yaml", "nginx", "ini"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path, fileType, lang string
		matched              bool
	}{
		{"app.conf", "code", "toml", true},
		{"deploy/app.conf", "code", "yaml", true},
		{"other/app.conf", "code", "ini", true},
		{"docs/guide.txt", "markdown", "", true},
		{"main.go", "", "", false},
		// An exact name beats a glob, even a longer one.
		{"nginx.conf", "code", "nginx", true},
		{"etc/nginx.conf", "code", "nginx", true},
		// Patterns match case-insensitively, like extension detection.
		{"Report.QMD", "markdown", "", true},
		{"README.MD", "code", "text", true},
		// "auto" keeps detection: no file type or language from the rules.
		{"vendor/x.conf", "", "", false},
	}
	for _, tc := range tests {
		fileType, ok := rules.FileType(tc.path)
		if fileType != tc.fileType || ok != tc.matched {
			t.Errorf("FileType(%q) = %q, %v; want %q, %v", tc.path, fileType, ok, tc.fileType, tc.matched)
		}
		if got := rules.Lang(tc.path); got != tc.lang {
			t.Errorf("Lang(%q) = %q, want %q", tc.path, got, tc.lang)
		}
	}
}

func TestMergeConfigs_LanguagesUnionProjectWins(t *testing.T) {
	global := Config{Languages: map[string]string{"*.conf": "toml", "*.qmd": "markdown"}}
	project := Config{Languages: map[string]string{"*.conf": "yaml", "*.gts": "glimmer-ts"}}
	merged := mergeConfigs(global, project, ConfigPresence{})
	want := map[string]string{"*.conf": "yaml", "*.qmd": "markdown", "*.gts": "glimmer-ts"}
	if len(merged.Languages) != len(want) {
		t.Fatalf("merged languages = %v, want %v", merged.Languages, want)
	}
	for k, v := range want {
		if merged.Languages[k] != v {
			t.Errorf("merged[%q] = %q, want %q", k, merged.Languages[k], v)
		}
	}
	if global.Languages["*.conf"] != "toml" {
		t.Errorf("merge mutated the global map: %v", global.Languages)
	}
}
