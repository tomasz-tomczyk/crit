package daemon

import "testing"

func TestResolvedCWD(t *testing.T) {
	want, err := ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	if want == "" {
		t.Fatal("expected non-empty resolved cwd")
	}
}
