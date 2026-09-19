package preview

import (
	"os"
	"reflect"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

func TestBuildPreviewStartArgs(t *testing.T) {
	unsetEnvForTest(t, "CRIT_SHARE_URL")
	tests := []struct {
		name           string
		port           int
		host           string
		publicURL      string
		allowUnauthNet bool
		noOpen         bool
		quiet          bool
		shareURL       string
		cfg            config.Config
		want           []string
	}{
		{
			name:      "CLI public URL overrides config",
			publicURL: "https://cli.ts.net",
			noOpen:    true,
			cfg:       config.Config{PublicURL: "https://config.ts.net"},
			want: []string{
				"--preview-file", "/tmp/page.html",
				"--public-url", "https://cli.ts.net",
				"--no-open",
				"--share-url", "https://crit.md",
			},
		},
		{
			name: "config public URL",
			cfg:  config.Config{PublicURL: "https://config.ts.net"},
			want: []string{
				"--preview-file", "/tmp/page.html",
				"--public-url", "https://config.ts.net",
				"--share-url", "https://crit.md",
			},
		},
		{
			name: "all explicit flags",
			port: 4242, host: "0.0.0.0", publicURL: "https://preview.example/base/",
			allowUnauthNet: true, noOpen: true, quiet: true,
			shareURL: "https://share.example/",
			want: []string{
				"--preview-file", "/tmp/page.html",
				"--port", "4242",
				"--host", "0.0.0.0",
				"--public-url", "https://preview.example/base/",
				"--allow-unauthenticated-network",
				"--no-open",
				"--quiet",
				"--share-url", "https://share.example",
			},
		},
		{
			name:   "share targets only",
			noOpen: true,
			cfg: config.Config{ShareTargets: []config.ShareTarget{
				{Name: "acme", URL: "https://crit.acme.example", Default: true},
			}},
			want: []string{
				"--preview-file", "/tmp/page.html",
				"--no-open",
				"--share-url", "https://crit.acme.example",
			},
		},
		{
			name: "ambiguous share targets omitted",
			cfg: config.Config{ShareTargets: []config.ShareTarget{
				{Name: "one", URL: "https://one.example"},
				{Name: "two", URL: "https://two.example"},
			}},
			want: []string{"--preview-file", "/tmp/page.html"},
		},
		{
			name:     "invalid explicit share URL omitted",
			shareURL: "://bad-url",
			want:     []string{"--preview-file", "/tmp/page.html"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPreviewStartArgsForTest(
				"/tmp/page.html", tt.port, tt.host, tt.publicURL, tt.allowUnauthNet,
				tt.noOpen, tt.quiet, tt.shareURL, tt.cfg,
			)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildPreviewStartArgs() = %v, want %v", got, tt.want)
			}
		})
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
			if err := os.Setenv(key, old); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
		} else {
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
		}
	})
}
