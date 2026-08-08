package github

import "testing"

func TestGitHubChangeNumber(t *testing.T) {
	for _, input := range []string{"42", "https://github.com/acme/widget/pull/42", "https://github.example/acme/widget/pull/42/files"} {
		got, err := githubChangeNumber(input)
		if err != nil || got != 42 {
			t.Errorf("githubChangeNumber(%q) = (%d, %v)", input, got, err)
		}
	}
}
