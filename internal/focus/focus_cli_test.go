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

func TestResolveFocus_PRAndRangeMutuallyExclusive(t *testing.T) {
	_, err := ResolveFocus("1", "a..b", "", false, nil, "")
	if err == nil {
		t.Fatal("expected error from mutually-exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error %q missing 'mutually exclusive'", err)
	}
}

func TestResolveFocus_RangeWithoutVCS(t *testing.T) {
	f, err := ResolveFocus("", "abc..def", "", false, nil, "")
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
	f, err := ResolveFocus("", "", "", false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Errorf("expected nil focus, got %+v", f)
	}
}

func TestResolveFocus_InvalidScopeRejected(t *testing.T) {
	_, err := ResolveFocus("", "a..b", "bogus", false, nil, "")
	if err == nil {
		t.Fatal("expected error from invalid scope")
	}
}
