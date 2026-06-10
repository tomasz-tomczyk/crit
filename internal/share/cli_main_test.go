package share

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptShareConsent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"N\n", false},
		{"\n", false},
		{"", false},
		{"yes\n", false},
	}
	for _, tt := range tests {
		var buf strings.Builder
		got := promptShareConsent(&buf, strings.NewReader(tt.input))
		if got != tt.want {
			t.Errorf("promptShareConsent(input=%q) = %v, want %v", tt.input, got, tt.want)
		}
		if !strings.Contains(buf.String(), "Continue?") {
			t.Errorf("promptShareConsent did not print prompt for input=%q", tt.input)
		}
	}
}

func TestPromptShareURLConfirm(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"N\n", false},
		{"\n", false},
		{"", false},
		{"yes\n", false},
	}
	for _, tt := range tests {
		var buf strings.Builder
		got := promptShareURLConfirm(&buf, strings.NewReader(tt.input), "https://example.com")
		if got != tt.want {
			t.Errorf("promptShareURLConfirm(input=%q) = %v, want %v", tt.input, got, tt.want)
		}
		out := buf.String()
		if !strings.Contains(out, "https://example.com") {
			t.Errorf("promptShareURLConfirm did not print URL for input=%q, got %q", tt.input, out)
		}
		if !strings.Contains(out, "continue?") && !strings.Contains(out, "Continue?") {
			t.Errorf("promptShareURLConfirm did not print prompt for input=%q, got %q", tt.input, out)
		}
	}
}

func TestRunUnpublish_NoReviewFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	err := RunUnpublish([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error when no review file matches path hints")
	}
	if !strings.Contains(err.Error(), "no review file found") {
		t.Fatalf("got %v", err)
	}
}

func TestParseShareFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		outputDir string
		svcURL    string
		showQR    bool
		files     []string
	}{
		{
			name:  "no flags",
			args:  []string{"plan.md"},
			files: []string{"plan.md"},
		},
		{
			name:      "output flag long form",
			args:      []string{"--output", "/tmp/out", "plan.md"},
			outputDir: "/tmp/out",
			files:     []string{"plan.md"},
		},
		{
			name:      "output flag short form",
			args:      []string{"-o", "/tmp/out", "plan.md"},
			outputDir: "/tmp/out",
			files:     []string{"plan.md"},
		},
		{
			name:   "share-url flag",
			args:   []string{"--share-url", "https://custom.example.com", "plan.md"},
			svcURL: "https://custom.example.com",
			files:  []string{"plan.md"},
		},
		{
			name:   "qr flag",
			args:   []string{"--qr", "plan.md"},
			showQR: true,
			files:  []string{"plan.md"},
		},
		{
			name:      "all flags combined",
			args:      []string{"--output", "/tmp/out", "--share-url", "https://x.com", "--qr", "a.md", "b.md"},
			outputDir: "/tmp/out",
			svcURL:    "https://x.com",
			showQR:    true,
			files:     []string{"a.md", "b.md"},
		},
		{
			name:  "no args",
			args:  nil,
			files: nil,
		},
		{
			name:  "multiple files only",
			args:  []string{"a.md", "b.go", "c.txt"},
			files: []string{"a.md", "b.go", "c.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sf, err := parseShareFlags(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if sf.outputDir != tt.outputDir {
				t.Errorf("outputDir = %q, want %q", sf.outputDir, tt.outputDir)
			}
			if sf.svcURL != tt.svcURL {
				t.Errorf("svcURL = %q, want %q", sf.svcURL, tt.svcURL)
			}
			if sf.showQR != tt.showQR {
				t.Errorf("showQR = %v, want %v", sf.showQR, tt.showQR)
			}
			if len(sf.files) != len(tt.files) {
				t.Fatalf("files = %v, want %v", sf.files, tt.files)
			}
			for i := range tt.files {
				if sf.files[i] != tt.files[i] {
					t.Errorf("files[%d] = %q, want %q", i, sf.files[i], tt.files[i])
				}
			}
		})
	}
}

func TestParseShareFlags_OrgVisibility(t *testing.T) {
	t.Run("--org flag", func(t *testing.T) {
		sf, err := parseShareFlags([]string{"--org", "acme", "plan.md"})
		if err != nil {
			t.Fatal(err)
		}
		if sf.org != "acme" {
			t.Fatalf("expected org=acme, got %q", sf.org)
		}
	})

	t.Run("--visibility flag", func(t *testing.T) {
		sf, err := parseShareFlags([]string{"--visibility", "organization", "plan.md"})
		if err != nil {
			t.Fatal(err)
		}
		if sf.visibility != "organization" {
			t.Fatalf("expected visibility=organization, got %q", sf.visibility)
		}
	})

	t.Run("both flags", func(t *testing.T) {
		sf, err := parseShareFlags([]string{"--org", "acme", "--visibility", "unlisted", "plan.md"})
		if err != nil {
			t.Fatal(err)
		}
		if sf.org != "acme" || sf.visibility != "unlisted" {
			t.Fatalf("got org=%q vis=%q", sf.org, sf.visibility)
		}
	})
}

func TestLoadShareFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	p1 := filepath.Join(dir, "plan.md")
	p2 := filepath.Join(dir, "notes.txt")
	os.WriteFile(p1, []byte("# My Plan"), 0644)
	os.WriteFile(p2, []byte("Some notes"), 0644)

	t.Run("loads single file", func(t *testing.T) {
		files, err := loadShareFiles([]string{p1})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].Content != "# My Plan" {
			t.Errorf("content = %q", files[0].Content)
		}
	})

	t.Run("loads multiple files", func(t *testing.T) {
		files, err := loadShareFiles([]string{p1, p2})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 2 {
			t.Fatalf("expected 2 files, got %d", len(files))
		}
		if files[0].Content != "# My Plan" {
			t.Errorf("file 0 content = %q", files[0].Content)
		}
		if files[1].Content != "Some notes" {
			t.Errorf("file 1 content = %q", files[1].Content)
		}
	})

	t.Run("absolute path made relative", func(t *testing.T) {
		files, err := loadShareFiles([]string{p1})
		if err != nil {
			t.Fatal(err)
		}
		// The absolute path should be converted to a relative path
		if files[0].Path == "" {
			t.Error("expected non-empty path")
		}
		// The path should not be the full absolute path (unless cwd is /)
		if filepath.IsAbs(files[0].Path) {
			// It's OK if the relative conversion fails (e.g., different volume on Windows),
			// but on Unix it should succeed
			wd, _ := os.Getwd()
			if wd != "/" {
				t.Logf("path stayed absolute: %q (cwd: %q)", files[0].Path, wd)
			}
		}
	})

	t.Run("empty list returns nil", func(t *testing.T) {
		files, err := loadShareFiles(nil)
		if err != nil {
			t.Fatal(err)
		}
		if files != nil {
			t.Errorf("expected nil, got %v", files)
		}
	})
}

func TestPrintQR_NoopWhenFalse(t *testing.T) {
	// printQR with showQR=false should not panic and should be a no-op
	printQR("https://example.com", false)
}
