package preview

import (
	"os"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

func TestBuildPreviewStartArgs_PublicURL(t *testing.T) {
	unsetEnvForTest(t, "CRIT_SHARE_URL")
	cfg := config.Config{PublicURL: "https://config.ts.net"}
	args := buildPreviewStartArgsForTest("/tmp/page.html", 0, "", "https://cli.ts.net", false, true, false, "", cfg)
	want := []string{
		"--preview-file", "/tmp/page.html",
		"--public-url", "https://cli.ts.net",
		"--no-open",
		"--share-url", "https://crit.md",
	}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}

	fromCfg := buildPreviewStartArgsForTest("/tmp/page.html", 0, "", "", false, false, false, "", cfg)
	if len(fromCfg) != 6 || fromCfg[2] != "--public-url" || fromCfg[3] != "https://config.ts.net" {
		t.Fatalf("config public-url: got %v", fromCfg)
	}
}

func TestBuildPreviewStartArgs_ShareTargetsOnly(t *testing.T) {
	unsetEnvForTest(t, "CRIT_SHARE_URL")
	cfg := config.Config{
		ShareTargets: []config.ShareTarget{
			{Name: "acme", URL: "https://crit.acme.example", Default: true},
		},
	}
	args := buildPreviewStartArgsForTest("/tmp/page.html", 0, "", "", false, true, false, "", cfg)
	want := []string{
		"--preview-file", "/tmp/page.html",
		"--no-open",
		"--share-url", "https://crit.acme.example",
	}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, args[i], want[i])
		}
	}
	for _, a := range args {
		if a == "https://crit.md" {
			t.Fatalf("share_targets-only config must not inject DefaultShareURL; got %v", args)
		}
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
