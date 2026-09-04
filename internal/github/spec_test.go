package github

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func TestParsePRSpec(t *testing.T) {
	cases := []struct {
		in      string
		want    forge.ChangeID
		wantErr bool
	}{
		{"295", forge.ChangeID{Number: 295}, false},
		{
			"https://github.com/a/b/pull/295",
			forge.ChangeID{Number: 295, Project: "a/b", Host: "github.com"},
			false,
		},
		{
			"https://github.com/a/b/pull/295/files",
			forge.ChangeID{Number: 295, Project: "a/b", Host: "github.com"},
			false,
		},
		{
			"https://github.com/myorg/repo-b/pull/1?diff=split",
			forge.ChangeID{Number: 1, Project: "myorg/repo-b", Host: "github.com"},
			false,
		},
		{
			"https://www.github.com/o/r/pull/2",
			forge.ChangeID{Number: 2, Project: "o/r", Host: "github.com"},
			false,
		},
		{
			"https://github.example.com/acme/app/pull/9",
			forge.ChangeID{Number: 9, Project: "acme/app", Host: "github.example.com"},
			false,
		},
		{"http://github.com/o/r/pull/7", forge.ChangeID{Number: 7, Project: "o/r", Host: "github.com"}, false},
		{"abc", forge.ChangeID{}, true},
		{"-5", forge.ChangeID{}, true},
		{"0", forge.ChangeID{}, true},
		{"", forge.ChangeID{}, true},
		{"https://github.com/a/b/issues/295", forge.ChangeID{}, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParsePRSpec(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}
