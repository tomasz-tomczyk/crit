package main

import "testing"

// TestIsGHAuthFailure exercises the line-anchored 401 detector. We accept
// only gh's canonical framings (stderr "gh: HTTP 401:" and the
// --include status line "HTTP/x 401") and explicitly reject bare
// "HTTP 401" / "Bad credentials" substrings to avoid false positives
// from echoed request bodies.
func TestIsGHAuthFailure(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "gh canonical stderr line",
			in:   "gh: HTTP 401: Bad credentials",
			want: true,
		},
		{
			name: "gh canonical stderr line with trailing newline",
			in:   "gh: HTTP 401: Bad credentials (https://api.github.com/repos/x/y)\n",
			want: true,
		},
		{
			name: "include block HTTP/1.1 401 at line start",
			in:   "HTTP/1.1 401 Unauthorized\nDate: Mon\nX-RateLimit-Remaining: 0\n",
			want: true,
		},
		{
			name: "include block HTTP/2 401 at line start",
			in:   "HTTP/2 401\nContent-Type: application/json\n",
			want: true,
		},
		{
			name: "include block HTTP/2.0 401 at line start",
			in:   "HTTP/2.0 401\n",
			want: true,
		},
		{
			name: "echoed body containing HTTP 401 mid-line — must not trigger",
			in:   "creating review: failed: got HTTP 401 from upstream service",
			want: false,
		},
		{
			name: "Bad credentials mid-line in non-gh context — must not trigger",
			in:   "comment body: \"the error said Bad credentials but it's a typo\"",
			want: false,
		},
		{
			name: "200 OK include block",
			in:   "HTTP/2 200\nContent-Type: application/json\n",
			want: false,
		},
		{
			name: "404 not-found include block",
			in:   "HTTP/1.1 404 Not Found\n",
			want: false,
		},
		{
			name: "empty",
			in:   "",
			want: false,
		},
		{
			name: "indented HTTP/1.1 401 (not at line start) — must not trigger",
			in:   "  HTTP/1.1 401 Unauthorized\n",
			want: false,
		},
		{
			name: "indented gh: line — must not trigger",
			in:   "  gh: HTTP 401: Bad credentials\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGHAuthFailure([]byte(tt.in))
			if got != tt.want {
				t.Errorf("isGHAuthFailure(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
