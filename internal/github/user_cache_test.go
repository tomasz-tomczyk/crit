package github

import "testing"

func TestUserNameCache_LookupEmpty(t *testing.T) {
	c := userNameCache{}
	if got := c.lookup(""); got != "" {
		t.Errorf("lookup(\"\") = %q, want empty", got)
	}
}

func TestUserNameCache_CacheHit(t *testing.T) {
	c := userNameCache{"alice": "Alice Liddell"}
	if got := c.lookup("alice"); got != "Alice Liddell" {
		t.Errorf("lookup = %q, want cached name", got)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		version string
		major   int
		minor   int
		patch   int
		want    bool
	}{
		{"2.48.0", 2, 48, 0, true},
		{"2.47.9", 2, 48, 0, false},
		{"3.0.0", 2, 48, 0, true},
		{"2.49.0", 2, 48, 0, true},
		{"v2.48.1", 2, 48, 0, true},
		{"2.48.0-rc1", 2, 48, 0, true},
		{"2.48", 2, 48, 0, false},
		{"not-a-version", 2, 48, 0, false},
	}
	for _, c := range cases {
		if got := versionAtLeast(c.version, c.major, c.minor, c.patch); got != c.want {
			t.Errorf("versionAtLeast(%q, %d,%d,%d) = %v, want %v", c.version, c.major, c.minor, c.patch, got, c.want)
		}
	}
}
