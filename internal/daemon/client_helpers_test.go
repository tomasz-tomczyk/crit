package daemon

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestJoinParts(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"one"}, "one"},
		{[]string{"one", "two"}, "one and two"},
		{[]string{"a", "b", "c"}, "a and b, and c"},
	}
	for _, c := range cases {
		if got := joinParts(c.in); got != c.want {
			t.Errorf("joinParts(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPrintSessionSummary_SkipsEmpty(t *testing.T) {
	out := captureStderr(t, func() {
		printSessionSummary(&struct {
			Duration int `json:"duration_seconds"`
			Files    int `json:"files_reviewed"`
			Comments int `json:"comments_submitted"`
		}{})
	})
	if out != "" {
		t.Errorf("empty summary should print nothing, got %q", out)
	}
}

func TestPrintSessionSummary_FormatsParts(t *testing.T) {
	out := captureStderr(t, func() {
		printSessionSummary(&struct {
			Duration int `json:"duration_seconds"`
			Files    int `json:"files_reviewed"`
			Comments int `json:"comments_submitted"`
		}{Files: 2, Comments: 1, Duration: 30})
	})
	if !strings.Contains(out, "2 files") {
		t.Errorf("expected files count in %q", out)
	}
	if !strings.Contains(out, "1 comment") {
		t.Errorf("expected singular comment in %q", out)
	}
	if !strings.Contains(out, "30s") {
		t.Errorf("expected duration in %q", out)
	}
}

func TestReadReviewCycleResponse_OK(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"approved":true}`)),
	}
	body, err := readReviewCycleResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"approved":true}` {
		t.Errorf("got %q", body)
	}
}

func TestReadReviewCycleResponse_GatewayTimeout(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	_, err := readReviewCycleResponse(resp)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
