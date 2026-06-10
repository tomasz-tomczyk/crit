package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirArgs(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		paths []string
		want  int
	}{
		{"empty", nil, 0},
		{"files only", []string{file}, 0},
		{"dirs only", []string{dir}, 1},
		{"mixed", []string{file, dir}, 1},
		{"nonexistent", []string{"/no/such/path"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirArgs(tt.paths)
			if len(got) != tt.want {
				t.Errorf("dirArgs(%v) returned %d dirs, want %d", tt.paths, len(got), tt.want)
			}
		})
	}
}
