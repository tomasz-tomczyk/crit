package focus

import (
	"strings"
	"testing"
)

func TestParsePRSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"295", 295, false},
		{"https://github.com/a/b/pull/295", 295, false},
		{"https://github.com/a/b/pull/295/files", 295, false},
		{"https://github.com/a/b/pull/295?diff=split", 295, false},
		{"abc", 0, true},
		{"-5", 0, true},
		{"0", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParsePRSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("err=%v wantErr=%v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestLooksLikePRURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://github.com/a/b/pull/295", true},
		{"https://github.com/a/b/pull/295/files", true},
		{"http://github.com/a/b/pull/1", true},
		{"https://example.com", false},
		{"https://github.com/a/b/issues/295", false},
		{"295", false},
		{"README.md", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := LooksLikePRURL(c.in); got != c.want {
				t.Errorf("LooksLikePRURL(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseRangeSpec(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantHead string
		wantErr  bool
	}{
		{"abc..def", "abc", "def", false},
		{"main..feature-x", "main", "feature-x", false},
		{"abc...def", "", "", true},
		{"abc", "", "", true},
		{"..def", "", "", true},
		{"abc..", "", "", true},
		{"", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			b, h, err := ParseRangeSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if b != c.wantBase || h != c.wantHead {
				t.Errorf("got (%q, %q) want (%q, %q)", b, h, c.wantBase, c.wantHead)
			}
		})
	}
}

func TestParseScopeSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    DiffScope
		wantErr bool
	}{
		{"", DiffScopeLayer, false},
		{"layer", DiffScopeLayer, false},
		{"full-stack", DiffScopeFullStack, false},
		{"full_stack", DiffScopeFullStack, false},
		{"bogus", "", true},
		{"working-tree", "", true},
		{"working_tree", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseScopeSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("err=%v wantErr=%v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestResolveFocus_ChangeAndRangeMutuallyExclusive(t *testing.T) {
	_, err := ResolveFocus(ChangeSpec{Forge: "github", Value: "1"}, "a..b", "", false, nil, "")
	if err == nil {
		t.Fatal("expected error from mutually-exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q missing 'mutually exclusive'", err)
	}
}

func TestResolveFocus_RejectsIncompleteChangeSpec(t *testing.T) {
	for _, change := range []ChangeSpec{
		{Forge: "github"},
		{Value: "42"},
	} {
		if _, err := ResolveFocus(change, "", "", false, nil, ""); err == nil {
			t.Fatalf("expected error for incomplete change spec %+v", change)
		}
	}
}

func TestResolveFocus_RejectsUnsupportedForge(t *testing.T) {
	_, err := ResolveFocus(ChangeSpec{Forge: "other", Value: "9"}, "", "", false, nil, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported change forge") {
		t.Fatalf("expected unsupported forge error, got %v", err)
	}
}

func TestResolveFocus_RangeWithoutVCS(t *testing.T) {
	f, err := ResolveFocus(ChangeSpec{}, "abc..def", "", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.Kind != FocusRange {
		t.Fatalf("got %+v want range focus", f)
	}
	if f.BaseSHA != "abc" || f.HeadSHA != "def" {
		t.Errorf("got base=%q head=%q want abc/def", f.BaseSHA, f.HeadSHA)
	}
	if f.DiffScope != DiffScopeLayer {
		t.Errorf("default scope should be layer, got %q", f.DiffScope)
	}
}

func TestResolveFocus_NilWhenNoFlags(t *testing.T) {
	f, err := ResolveFocus(ChangeSpec{}, "", "", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Errorf("expected nil focus, got %+v", f)
	}
}

func TestResolveFocus_InvalidScopeRejected(t *testing.T) {
	_, err := ResolveFocus(ChangeSpec{}, "a..b", "bogus", false, nil, "")
	if err == nil {
		t.Fatal("expected error from invalid scope")
	}
}
