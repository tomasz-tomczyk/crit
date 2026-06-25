package preview

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

func TestBuildPreviewStartArgs_PublicURL(t *testing.T) {
	cfg := config.Config{PublicURL: "https://config.ts.net"}
	args := buildPreviewStartArgsForTest("/tmp/page.html", 0, "", "https://cli.ts.net", true, false, "", cfg)
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

	fromCfg := buildPreviewStartArgsForTest("/tmp/page.html", 0, "", "", false, false, "", cfg)
	if len(fromCfg) != 6 || fromCfg[2] != "--public-url" || fromCfg[3] != "https://config.ts.net" {
		t.Fatalf("config public-url: got %v", fromCfg)
	}
}
