package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLiveCookies_FlagsOnly(t *testing.T) {
	got, err := resolveLiveCookies([]string{"session=abc", "other=def"}, "", Config{}, "")
	if err != nil {
		t.Fatalf("resolveLiveCookies: %v", err)
	}
	want := "session=abc; other=def"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveLiveCookies_ConfigAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(path, []byte("from_file=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{LiveCookie: "from_cfg=1", LiveCookieFile: path}
	got, err := resolveLiveCookies([]string{"from_flag=1"}, "", cfg, dir)
	if err != nil {
		t.Fatalf("resolveLiveCookies: %v", err)
	}
	want := "from_flag=1; from_cfg=1; from_file=1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveLiveCookies_FlagFileOverridesConfigFile(t *testing.T) {
	dir := t.TempDir()
	flagPath := filepath.Join(dir, "flag.txt")
	cfgPath := filepath.Join(dir, "cfg.txt")
	if err := os.WriteFile(flagPath, []byte("flag_file=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("cfg_file=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveLiveCookies(nil, flagPath, Config{LiveCookieFile: cfgPath}, dir)
	if err != nil {
		t.Fatalf("resolveLiveCookies: %v", err)
	}
	want := "flag_file=1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadLiveCookieFile_Netscape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jar.txt")
	content := "# Netscape HTTP Cookie File\n" +
		".example.com\tTRUE\t/\tFALSE\t0\tsession\tabc123\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readLiveCookieFile(path)
	if err != nil {
		t.Fatalf("readLiveCookieFile: %v", err)
	}
	if got != "session=abc123" {
		t.Fatalf("got %q, want session=abc123", got)
	}
}

func TestResolveLiveCookies_ProjectRelativeCookieFile(t *testing.T) {
	dir := t.TempDir()
	cookiePath := filepath.Join(dir, ".crit", "live-cookies.txt")
	if err := os.MkdirAll(filepath.Dir(cookiePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cookiePath, []byte("session=from_project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{LiveCookieFile: ".crit/live-cookies.txt"}
	got, err := resolveLiveCookies(nil, "", cfg, dir)
	if err != nil {
		t.Fatalf("resolveLiveCookies: %v", err)
	}
	if got != "session=from_project" {
		t.Fatalf("got %q, want session=from_project", got)
	}
}

func TestParseServerFlags_LiveCookie(t *testing.T) {
	f := parseServerFlags([]string{"--live-origin", "http://localhost:3000", "--live-cookie", "session=abc"})
	if f.liveCookie != "session=abc" {
		t.Fatalf("liveCookie = %q, want session=abc", f.liveCookie)
	}
}
