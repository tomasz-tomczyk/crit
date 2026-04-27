package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// decodeAiderConf helper unmarshals an aider conf file into a generic map
// for assertion. Returns a fresh map per call.
func decodeAiderConf(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode yaml: %v", err)
	}
	return out
}

func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any for read, got %T", v)
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("expected string in read list, got %T", x)
		}
		out = append(out, s)
	}
	return out
}

func TestMergeAiderConfYAML_EmptyDocument(t *testing.T) {
	out, err := mergeAiderConfYAML(nil, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, out)
	got := toStringSlice(t, conf["read"])
	if len(got) != 1 || got[0] != ".crit/aider-conventions.md" {
		t.Errorf("expected single entry, got %v", got)
	}
	if len(conf) != 1 {
		t.Errorf("expected only read key, got %v", conf)
	}
}

func TestMergeAiderConfYAML_PreservesExistingKeys(t *testing.T) {
	input := []byte(`model: gpt-4
auto-commits: false
read:
  - foo.md
  - bar.md
`)
	out, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, out)

	if conf["model"] != "gpt-4" {
		t.Errorf("model not preserved: got %v", conf["model"])
	}
	if conf["auto-commits"] != false {
		t.Errorf("auto-commits not preserved: got %v", conf["auto-commits"])
	}
	got := toStringSlice(t, conf["read"])
	want := []string{"foo.md", "bar.md", ".crit/aider-conventions.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("read list mismatch:\n got:  %v\n want: %v", got, want)
	}
}

func TestMergeAiderConfYAML_Idempotent(t *testing.T) {
	input := []byte(`model: gpt-4
read:
  - foo.md
`)
	first, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mergeAiderConfYAML(first, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	conf := decodeAiderConf(t, second)
	got := toStringSlice(t, conf["read"])
	count := 0
	for _, s := range got {
		if s == ".crit/aider-conventions.md" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one .crit entry, got %d in %v", count, got)
	}
}

func TestMergeAiderConfYAML_AlreadyPresent(t *testing.T) {
	input := []byte(`read:
  - .crit/aider-conventions.md
`)
	out, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, out)
	got := toStringSlice(t, conf["read"])
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d: %v", len(got), got)
	}
}

func TestMergeAiderConfYAML_ScalarReadPromotedToSequence(t *testing.T) {
	input := []byte(`read: foo.md
model: gpt-4
`)
	out, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, out)
	got := toStringSlice(t, conf["read"])
	want := []string{"foo.md", ".crit/aider-conventions.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	if conf["model"] != "gpt-4" {
		t.Errorf("model not preserved: %v", conf["model"])
	}
}

func TestMergeAiderConfYAML_NoReadKey(t *testing.T) {
	input := []byte(`model: gpt-4
`)
	out, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, out)
	got := toStringSlice(t, conf["read"])
	if len(got) != 1 || got[0] != ".crit/aider-conventions.md" {
		t.Errorf("expected single entry, got %v", got)
	}
	if conf["model"] != "gpt-4" {
		t.Errorf("model not preserved: %v", conf["model"])
	}
}

func TestMergeAiderConfFile_CreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aider.conf.yml")

	if err := mergeAiderConfFile(path, ".crit/aider-conventions.md"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, data)
	got := toStringSlice(t, conf["read"])
	if len(got) != 1 || got[0] != ".crit/aider-conventions.md" {
		t.Errorf("expected single entry, got %v", got)
	}
}

func TestMergeAiderConfFile_PreservesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aider.conf.yml")
	original := []byte(`model: gpt-4
auto-commits: false
read:
  - foo.md
  - bar.md
`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run install twice — second call should be a no-op for content.
	if err := mergeAiderConfFile(path, ".crit/aider-conventions.md"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := mergeAiderConfFile(path, ".crit/aider-conventions.md"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("file not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	conf := decodeAiderConf(t, second)
	if conf["model"] != "gpt-4" {
		t.Errorf("model not preserved: %v", conf["model"])
	}
	if conf["auto-commits"] != false {
		t.Errorf("auto-commits not preserved: %v", conf["auto-commits"])
	}
	got := toStringSlice(t, conf["read"])
	want := []string{"foo.md", "bar.md", ".crit/aider-conventions.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("read list mismatch:\n got:  %v\n want: %v", got, want)
	}
}

func TestAiderPaths_ProjectVsGlobal(t *testing.T) {
	cwd := "/tmp/proj"
	home := "/home/me"

	p := aiderPaths(cwd, home)
	if p.conventionsDest != filepath.Join(cwd, ".crit", "aider-conventions.md") {
		t.Errorf("project conventionsDest: %s", p.conventionsDest)
	}
	if p.confPath != filepath.Join(cwd, ".aider.conf.yml") {
		t.Errorf("project confPath: %s", p.confPath)
	}
	if p.readEntry != ".crit/aider-conventions.md" {
		t.Errorf("project readEntry: %s", p.readEntry)
	}

	g := aiderPaths(home, home)
	if g.conventionsDest != filepath.Join(home, ".crit-conventions.md") {
		t.Errorf("global conventionsDest: %s", g.conventionsDest)
	}
	if g.confPath != filepath.Join(home, ".aider.conf.yml") {
		t.Errorf("global confPath: %s", g.confPath)
	}
	if g.readEntry != "~/.crit-conventions.md" {
		t.Errorf("global readEntry: %s", g.readEntry)
	}
}

func TestIsGlobalInstall(t *testing.T) {
	if !isGlobalInstall("/home/me", "/home/me") {
		t.Error("equal paths should be global")
	}
	if isGlobalInstall("/home/me/proj", "/home/me") {
		t.Error("subdir should not be global")
	}
	if isGlobalInstall("", "/home/me") {
		t.Error("empty cwd should not be global")
	}
}

func TestResolveGlobalDest(t *testing.T) {
	home := "/home/me"

	// Plain relative-to-home.
	got, err := resolveGlobalDest(globalDestRelHome, ".agents/skills/crit/SKILL.md", home)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, ".agents/skills/crit/SKILL.md") {
		t.Errorf("got %s", got)
	}

	// Absolute kind — returned as-is.
	got, err = resolveGlobalDest(globalDestAbsolute, "/etc/foo", home)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/etc/foo" {
		t.Errorf("got %s", got)
	}

	// Documents kind — joined under platform Documents dir. We stub
	// xdg-user-dir to a deterministic path so the test works on Linux too.
	prev := xdgUserDirFn
	xdgUserDirFn = func(string) (string, error) { return "/home/me/Docs", nil }
	t.Cleanup(func() { xdgUserDirFn = prev })

	got, err = resolveGlobalDest(globalDestDocuments, "Cline/Rules/crit.md", home)
	if err != nil {
		t.Fatal(err)
	}
	docs := documentsDir(home)
	want := filepath.Join(docs, "Cline/Rules/crit.md")
	if got != want {
		t.Errorf("documents kind: got %s want %s", got, want)
	}
}

func TestDocumentsDir_LinuxFallbacks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only behavior")
	}
	home := "/home/me"
	prev := xdgUserDirFn
	t.Cleanup(func() { xdgUserDirFn = prev })

	// Sensible path → use it.
	xdgUserDirFn = func(string) (string, error) { return "/home/me/Documents-Custom", nil }
	if got := documentsDir(home); got != "/home/me/Documents-Custom" {
		t.Errorf("sensible path: got %s", got)
	}

	// xdg-user-dir returns $HOME (the spec quirk when user-dirs.dirs is
	// missing) → fall back to $HOME/Documents, NOT $HOME.
	xdgUserDirFn = func(string) (string, error) { return home, nil }
	if got := documentsDir(home); got != filepath.Join(home, "Documents") {
		t.Errorf("home quirk: got %s", got)
	}

	// Binary missing / errors → fall back to $HOME/Documents.
	xdgUserDirFn = func(string) (string, error) { return "", os.ErrNotExist }
	if got := documentsDir(home); got != filepath.Join(home, "Documents") {
		t.Errorf("missing binary: got %s", got)
	}
}

func TestInstallAider_ProjectMode(t *testing.T) {
	// Use a temp dir as cwd to exercise the full installAider flow.
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Pre-existing conf with other keys + read entries.
	confPath := filepath.Join(dir, ".aider.conf.yml")
	if err := os.WriteFile(confPath, []byte("model: gpt-4\nread:\n  - foo.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	installAider(false)

	// Conventions file written.
	convPath := filepath.Join(dir, ".crit", "aider-conventions.md")
	if _, err := os.Stat(convPath); err != nil {
		t.Errorf("conventions file not written: %v", err)
	}

	// Conf merged: foo.md + .crit/aider-conventions.md, model preserved.
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	conf := decodeAiderConf(t, data)
	if conf["model"] != "gpt-4" {
		t.Errorf("model lost: %v", conf["model"])
	}
	got := toStringSlice(t, conf["read"])
	want := []string{"foo.md", ".crit/aider-conventions.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("read list mismatch:\n got:  %v\n want: %v", got, want)
	}

	// Idempotent: a second install should not change the file.
	before, _ := os.ReadFile(confPath)
	installAider(false)
	after, _ := os.ReadFile(confPath)
	if string(before) != string(after) {
		t.Errorf("second install not idempotent:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInstallAiderAt_GlobalMode(t *testing.T) {
	// In global mode (cwd == home), conventions land at $HOME/.crit-conventions.md
	// and the conf is $HOME/.aider.conf.yml. Use a temp dir as both cwd and
	// home so we don't touch the real home dir.
	home := t.TempDir()

	if err := installAiderAt(home, home, false); err != nil {
		t.Fatalf("installAiderAt: %v", err)
	}

	convPath := filepath.Join(home, ".crit-conventions.md")
	if _, err := os.Stat(convPath); err != nil {
		t.Errorf("global conventions file not written: %v", err)
	}

	confPath := filepath.Join(home, ".aider.conf.yml")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("global conf not written: %v", err)
	}
	conf := decodeAiderConf(t, data)
	got := toStringSlice(t, conf["read"])
	if len(got) != 1 || got[0] != "~/.crit-conventions.md" {
		t.Errorf("expected single ~/.crit-conventions.md entry, got %v", got)
	}
}

func TestMergeAiderConfYAML_MultiDocRejected(t *testing.T) {
	input := []byte(`model: gpt-4
read:
  - foo.md
---
model: gpt-3
`)
	_, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err == nil {
		t.Fatal("expected error for multi-doc YAML, got nil")
	}
	if !strings.Contains(err.Error(), "multi-document") {
		t.Errorf("error should mention multi-document, got: %v", err)
	}
}

func TestMergeAiderConfFile_MultiDocLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aider.conf.yml")
	original := []byte("model: gpt-4\nread:\n  - foo.md\n---\nmodel: gpt-3\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	err := mergeAiderConfFile(path, ".crit/aider-conventions.md")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// File contents must be byte-for-byte identical.
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Errorf("file mutated despite error:\nbefore:\n%s\nafter:\n%s", original, after)
	}
}

func TestMergeAiderConfYAML_StripsBOM(t *testing.T) {
	input := append([]byte{0xEF, 0xBB, 0xBF}, []byte("model: gpt-4\nread:\n  - foo.md\n")...)
	out, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err != nil {
		t.Fatalf("BOM not handled: %v", err)
	}
	conf := decodeAiderConf(t, out)
	if conf["model"] != "gpt-4" {
		t.Errorf("model not preserved through BOM: %v", conf["model"])
	}
	got := toStringSlice(t, conf["read"])
	want := []string{"foo.md", ".crit/aider-conventions.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("read list mismatch:\n got:  %v\n want: %v", got, want)
	}
}

func TestMergeAiderConfYAML_MalformedReturnsError(t *testing.T) {
	// Tab indentation under a key is invalid in YAML.
	input := []byte("model: gpt-4\nread:\n\t- foo.md\n")
	_, err := mergeAiderConfYAML(input, ".crit/aider-conventions.md")
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestMergeAiderConfFile_MalformedLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".aider.conf.yml")
	original := []byte("model: gpt-4\nread:\n\t- foo.md\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeAiderConfFile(path, ".crit/aider-conventions.md"); err == nil {
		t.Fatal("expected error, got nil")
	}

	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Errorf("file mutated despite error")
	}
}

func TestWriteFileMkdirAtomic_CleansUpTempfile(t *testing.T) {
	// Atomic write: after a successful rename, no .tmp.* siblings should
	// remain. This indirectly proves rename happened (and didn't leave a
	// half-written file behind).
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "file.yml")

	if err := writeFileMkdirAtomic(path, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("contents: %q", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover tempfile: %s", e.Name())
		}
	}
}

func TestIsMultiDocYAML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"single doc", "model: gpt-4\nread:\n  - foo.md\n", false},
		{"leading marker only", "---\nmodel: gpt-4\n", false},
		{"two docs", "model: gpt-4\n---\nmodel: gpt-3\n", true},
		{"three docs", "a: 1\n---\nb: 2\n---\nc: 3\n", true},
		{"--- inside string not at line start", "msg: 'foo --- bar'\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMultiDocYAML([]byte(tc.in)); got != tc.want {
				t.Errorf("isMultiDocYAML(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
