package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePlanConfig_NameAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	os.WriteFile(path, []byte("# Test Plan"), 0644)

	pc := resolvePlanConfig([]string{"--name", "auth-flow", path})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != path {
		t.Errorf("filePath = %q, want %q", pc.filePath, path)
	}
}

func TestResolvePlanConfig_NameOnly(t *testing.T) {
	pc := resolvePlanConfig([]string{"--name", "auth-flow"})
	if pc.name != "auth-flow" {
		t.Errorf("name = %q, want %q", pc.name, "auth-flow")
	}
	if pc.filePath != "" {
		t.Errorf("filePath should be empty, got %q", pc.filePath)
	}
	if !pc.stdinExpected {
		t.Error("expected stdinExpected=true when no file arg")
	}
}

func TestResolvePlanSlug_UsesNameWhenProvided(t *testing.T) {
	slug := resolvePlanSlug("my-custom-name", []byte("# Some Heading"))
	if slug != "my-custom-name" {
		t.Errorf("resolvePlanSlug with name = %q, want my-custom-name", slug)
	}
}

func TestResolvePlanSlug_DerivesFromContent(t *testing.T) {
	slug := resolvePlanSlug("", []byte("# Auth Flow\n\nDetails here"))
	if slug == "" {
		t.Error("expected non-empty slug derived from content")
	}
	if !strings.Contains(slug, "auth-flow") {
		t.Errorf("slug = %q, expected to contain 'auth-flow'", slug)
	}
}
