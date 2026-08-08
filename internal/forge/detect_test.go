package forge

import "testing"

func TestDetectKind(t *testing.T) {
	tests := []struct {
		explicit string
		remote   string
		want     Kind
	}{
		{"gitlab", "https://github.com/acme/repo.git", GitLab},
		{"", "https://gitlab.com/acme/repo.git", GitLab},
		{"auto", "git@gitlab.example.com:group/repo.git", GitLab},
		{"", "https://github.com/acme/repo.git", GitHub},
		{"", "ssh://git@code.example.com/acme/repo.git", GitHub},
	}
	for _, tt := range tests {
		got, err := DetectKind(tt.explicit, tt.remote)
		if err != nil {
			t.Fatalf("DetectKind(%q, %q): %v", tt.explicit, tt.remote, err)
		}
		if got != tt.want {
			t.Errorf("DetectKind(%q, %q) = %q, want %q", tt.explicit, tt.remote, got, tt.want)
		}
	}
}

func TestRemoteHost(t *testing.T) {
	for input, want := range map[string]string{
		"git@code.example.com:group/project.git": "code.example.com",
		"https://gitlab.example/a/b.git":         "gitlab.example",
	} {
		if got := RemoteHost(input); got != want {
			t.Errorf("RemoteHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseKindRejectsUnknown(t *testing.T) {
	if _, err := ParseKind("bitbucket"); err == nil {
		t.Fatal("ParseKind(bitbucket) unexpectedly succeeded")
	}
}
