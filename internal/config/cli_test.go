package config

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunConfig_Help(t *testing.T) {
	err := RunConfig([]string{"--help"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunConfig_Generate(t *testing.T) {
	out := captureStdout(t, func() {
		if err := RunConfig([]string{"--generate"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"port"`) {
		t.Errorf("generate output should contain port key, got: %s", out[:min(200, len(out))])
	}
}

func TestRunConfig_ShowResolved(t *testing.T) {
	out := captureStdout(t, func() {
		if err := RunConfig(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "{") {
		t.Errorf("expected JSON config output, got: %s", out[:min(100, len(out))])
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
