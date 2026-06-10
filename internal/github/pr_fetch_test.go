package github

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestParsePRViewJSON(t *testing.T) {
	cases := []struct {
		name      string
		fixture   string
		wantBase  string
		wantHead  string
		wantFork  string
		wantCross bool
	}{
		{
			name: "same-repo PR",
			fixture: `{"number":295,"url":"https://github.com/a/b/pull/295","title":"x",
                "baseRefName":"main","headRefName":"feat",
                "baseRefOid":"abc","headRefOid":"def",
                "isCrossRepository":false,
                "headRepository":{"url":"https://github.com/a/b"},
                "author":{"login":"u"},"createdAt":"2026-04-28T00:00:00Z"}`,
			wantBase:  "abc",
			wantHead:  "def",
			wantFork:  "https://github.com/a/b",
			wantCross: false,
		},
		{
			name: "fork PR",
			fixture: `{"number":42,"url":"https://github.com/a/b/pull/42","title":"y",
                "baseRefName":"main","headRefName":"feat",
                "baseRefOid":"abc","headRefOid":"fork-sha",
                "isCrossRepository":true,
                "headRepository":{"url":"https://github.com/contributor/b"},
                "author":{"login":"c"},"createdAt":"2026-04-28T00:00:00Z"}`,
			wantBase:  "abc",
			wantHead:  "fork-sha",
			wantFork:  "https://github.com/contributor/b",
			wantCross: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := parsePRViewJSON([]byte(tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if info.BaseRefOid != tc.wantBase {
				t.Errorf("BaseRefOid: got %q want %q", info.BaseRefOid, tc.wantBase)
			}
			if info.HeadRefOid != tc.wantHead {
				t.Errorf("HeadRefOid: got %q want %q", info.HeadRefOid, tc.wantHead)
			}
			if info.HeadRepoURL != tc.wantFork {
				t.Errorf("HeadRepoURL: got %q want %q", info.HeadRepoURL, tc.wantFork)
			}
			if info.IsCrossRepository != tc.wantCross {
				t.Errorf("IsCrossRepository: got %v want %v", info.IsCrossRepository, tc.wantCross)
			}
		})
	}
}

func TestParsePRListJSON(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		wantLen int
		check   func(t *testing.T, prs []PRSummary)
	}{
		{
			name: "two PRs, one draft",
			fixture: `[
                {"number":1,"title":"a","url":"u1","headRefName":"r1","headRefOid":"s1","baseRefName":"main","isDraft":false},
                {"number":2,"title":"b","url":"u2","headRefName":"r2","headRefOid":"s2","baseRefName":"main","isDraft":true}
            ]`,
			wantLen: 2,
			check: func(t *testing.T, prs []PRSummary) {
				if prs[0].Number != 1 || prs[0].HeadRefOid != "s1" {
					t.Errorf("prs[0]: %+v", prs[0])
				}
				if !prs[1].IsDraft {
					t.Errorf("prs[1] should be draft: %+v", prs[1])
				}
			},
		},
		{
			name:    "empty list",
			fixture: `[]`,
			wantLen: 0,
			check:   func(t *testing.T, prs []PRSummary) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prs, err := parsePRListJSON([]byte(tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if len(prs) != tc.wantLen {
				t.Fatalf("len=%d want %d", len(prs), tc.wantLen)
			}
			tc.check(t, prs)
		})
	}
}

func TestParsePRListJSON_Malformed(t *testing.T) {
	if _, err := parsePRListJSON([]byte(`not json`)); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

// TestIsStackedPR is the truth-table for IsStackedPR(*PRInfo, vcs.VCS), exercised
// with a fake vcs.VCS so it runs without a real repo. P-3 / playwright reviewer
// wanted coverage of the production code path that synthetic e2e fixtures
// bypass by setting is_stacked explicitly.
func TestIsStackedPR(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		def     string
		nilInfo bool
		nilVCS  bool
		want    bool
	}{
		{name: "matches default", base: "main", def: "main", want: false},
		{name: "differs from default (stacked)", base: "feature-a", def: "main", want: true},
		{name: "nil info", def: "main", nilInfo: true, want: false},
		{name: "nil vcs", base: "main", nilVCS: true, want: false},
		{name: "empty default branch (graceful)", base: "main", def: "", want: false},
		{name: "empty default branch + non-empty base (graceful)", base: "feature-a", def: "", want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var info *PRInfo
			if !c.nilInfo {
				info = &PRInfo{BaseRefName: c.base}
			}
			var vcs vcs.VCS
			if !c.nilVCS {
				vcs = &fakeStackVCS{def: c.def}
			}
			if got := IsStackedPR(info, vcs); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

// fakeStackVCS implements vcs.VCS just enough for IsStackedPR/ensureSHAFetched tests.
type fakeStackVCS struct {
	vcs.VCS
	def      string
	name     string
	hasCalls int
	hasSeq   []bool
}

func (f *fakeStackVCS) DefaultBranch() string { return f.def }
func (f *fakeStackVCS) Name() string {
	if f.name == "" {
		return "git"
	}
	return f.name
}
func (f *fakeStackVCS) HasObject(_, _ string) bool {
	if f.hasCalls < len(f.hasSeq) {
		v := f.hasSeq[f.hasCalls]
		f.hasCalls++
		return v
	}
	return false
}

func TestEnsureSHAFetched_AlreadyPresent(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: []bool{true}}
	if err := ensureSHAFetched(v, "abc", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if v.hasCalls != 1 {
		t.Errorf("HasObject called %d times, want 1", v.hasCalls)
	}
}

func TestEnsureSHAFetched_StillMissingAfterFetch(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: nil} // always false
	err := ensureSHAFetched(v, "deadbeef", t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSHAFetched_NilVCSIsNoop(t *testing.T) {
	if err := ensureSHAFetched(nil, "abc", "", ""); err != nil {
		t.Errorf("nil vcs should no-op, got %v", err)
	}
}

func TestEnsureSHAFetched_UnsupportedVCS(t *testing.T) {
	v := &fakeStackVCS{name: "fossil", hasSeq: nil}
	err := ensureSHAFetched(v, "abc", t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for unsupported vcs")
	}
}
