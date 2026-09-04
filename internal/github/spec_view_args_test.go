package github

import (
	"reflect"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func TestPRViewArgs(t *testing.T) {
	cases := []struct {
		name string
		id   forge.ChangeID
		want []string
	}{
		{
			name: "bare number uses current checkout",
			id:   forge.ChangeID{Number: 42},
			want: []string{"pr", "view", "42", "--json", prJSONFields},
		},
		{
			name: "URL-derived project pins -R owner/repo",
			id:   forge.ChangeID{Number: 1, Project: "myorg/repo-b", Host: "github.com"},
			want: []string{"pr", "view", "1", "-R", "myorg/repo-b", "--json", prJSONFields},
		},
		{
			name: "enterprise host is included in -R",
			id:   forge.ChangeID{Number: 9, Project: "acme/app", Host: "github.example.com"},
			want: []string{"pr", "view", "9", "-R", "github.example.com/acme/app", "--json", prJSONFields},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := prViewArgs(c.id)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestPRCacheKey_IncludesEnterpriseHost(t *testing.T) {
	got := prCacheKey(forge.ChangeID{Number: 9, Project: "acme/app", Host: "github.example.com"})
	if got != "github.example.com/acme/app#9" {
		t.Fatalf("got %q", got)
	}
	dotcom := prCacheKey(forge.ChangeID{Number: 9, Project: "acme/app", Host: "github.com"})
	if dotcom != "acme/app#9" {
		t.Fatalf("github.com key = %q", dotcom)
	}
}
