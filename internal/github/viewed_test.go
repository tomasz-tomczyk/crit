package github

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestPlanViewedPush(t *testing.T) {
	const pr, head = 7, "head"
	mark := func(viewed bool) session.ViewedFile {
		return session.ViewedFile{Viewed: viewed, HeadSHA: head, ChangeNumber: pr}
	}
	never := func(string, string) bool { return false }
	always := func(string, string) bool { return true }
	tests := []struct {
		name   string
		entry  session.ViewedFile
		remote string // "" = file not in the PR
		stale  func(string, string) bool
		want   viewedPushPlan
	}{
		{name: "viewed locally", entry: mark(true), remote: "UNVIEWED", stale: never, want: viewedPushPlan{mark: []string{"a.go"}}},
		{name: "dismissed on github, current locally", entry: mark(true), remote: "DISMISSED", stale: never, want: viewedPushPlan{mark: []string{"a.go"}}},
		{name: "already viewed", entry: mark(true), remote: "VIEWED", stale: never},
		{name: "explicit untick", entry: mark(false), remote: "VIEWED", stale: never, want: viewedPushPlan{unmark: []string{"a.go"}}},
		{name: "untick of unviewed", entry: mark(false), remote: "UNVIEWED", stale: never},
		{name: "untick of dismissed", entry: mark(false), remote: "DISMISSED", stale: never},
		{name: "changed since viewed", entry: mark(true), remote: "DISMISSED", stale: always, want: viewedPushPlan{stale: []string{"a.go"}}},
		{name: "stale untick keeps newer github tick", entry: mark(false), remote: "VIEWED", stale: always, want: viewedPushPlan{stale: []string{"a.go"}}},
		{name: "auto marked", entry: session.ViewedFile{Viewed: true, Auto: true, HeadSHA: head, ChangeNumber: pr}, remote: "UNVIEWED", stale: never},
		{name: "marked outside a pr", entry: session.ViewedFile{Viewed: true}, remote: "UNVIEWED", stale: never},
		{name: "marked on another pr", entry: session.ViewedFile{Viewed: true, HeadSHA: head, ChangeNumber: 8}, remote: "UNVIEWED", stale: never},
		{name: "file not in pr", entry: mark(true), stale: never},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := prViewedState{headSHA: head, files: map[string]string{}}
			if tt.remote != "" {
				remote.files["a.go"] = tt.remote
			}
			got := planViewedPush(map[string]session.ViewedFile{"a.go": tt.entry}, pr, remote, tt.stale)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMergeRemoteViewed(t *testing.T) {
	cj := session.CritJSON{ViewedFiles: map[string]session.ViewedFile{
		"unticked.go": {Viewed: false},
	}}
	remote := prViewedState{headSHA: "head", files: map[string]string{
		"new.go":       "VIEWED",
		"unticked.go":  "VIEWED",
		"dismissed.go": "DISMISSED",
		"unviewed.go":  "UNVIEWED",
	}}
	if got := mergeRemoteViewed(&cj, 7, remote); got != 1 {
		t.Fatalf("added %d, want 1", got)
	}
	want := session.ViewedFile{Viewed: true, HeadSHA: "head", ChangeNumber: 7}
	if cj.ViewedFiles["new.go"] != want {
		t.Fatalf("got %+v", cj.ViewedFiles["new.go"])
	}
	if cj.ViewedFiles["unticked.go"].Viewed {
		t.Fatal("pull overrode a local untick")
	}
	if len(cj.ViewedFiles) != 2 {
		t.Fatalf("only VIEWED files are added: %+v", cj.ViewedFiles)
	}
}

// installViewedFakeGH puts a gh on PATH that runs body and logs its arguments,
// one call per line, to the returned file.
func installViewedFakeGH(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" | tr '\\n' ' ' >> '" + log + "'\necho >> '" + log + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestFetchPRViewedState_FollowsPages(t *testing.T) {
	log := installViewedFakeGH(t, `case "$*" in
  *after=c1*) echo '{"data":{"repository":{"pullRequest":{"id":"PR_1","headRefOid":"head","files":{"pageInfo":{"hasNextPage":false},"nodes":[{"path":"b.go","viewerViewedState":"UNVIEWED"}]}}}}}' ;;
  *) echo '{"data":{"repository":{"pullRequest":{"id":"PR_1","headRefOid":"head","files":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[{"path":"a.go","viewerViewedState":"VIEWED"}]}}}}}' ;;
esac`)
	got, err := fetchPRViewedState(forge.ChangeID{Number: 7, Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a.go": "VIEWED", "b.go": "UNVIEWED"}
	if got.nodeID != "PR_1" || got.headSHA != "head" || !reflect.DeepEqual(got.files, want) {
		t.Fatalf("got %+v", got)
	}
	data, _ := os.ReadFile(log)
	if n := strings.Count(string(data), "graphql"); n < 2 {
		t.Fatalf("expected two pages, log:\n%s", data)
	}
}

func TestCompareChangedFiles(t *testing.T) {
	files300 := strings.TrimSuffix(strings.Repeat(`{"filename":"x"},`, compareFileLimit), ",")
	tests := []struct {
		name, body string
		wantOK     bool
		wantPaths  []string
	}{
		{"ahead", `echo '{"status":"ahead","files":[{"filename":"new.go","previous_filename":"old.go"}]}'`, true, []string{"new.go", "old.go"}},
		{"identical", `echo '{"status":"identical","files":[]}'`, true, nil},
		{"force-pushed", `echo '{"status":"diverged","files":[]}'`, false, nil},
		{"head moved back", `echo '{"status":"behind","files":[]}'`, false, nil},
		{"list may be cut off", `echo '{"status":"ahead","files":[` + files300 + `]}'`, false, nil},
		{"commit gone", `exit 1`, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installViewedFakeGH(t, tt.body)
			paths, ok := compareChangedFiles(forge.ChangeID{Number: 7}, "old", "new")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			for _, p := range tt.wantPaths {
				if _, hit := paths[p]; !hit {
					t.Fatalf("missing %s in %v", p, paths)
				}
			}
		})
	}
}

func TestBuildViewedPushPlan_ComparesEachRevisionOnce(t *testing.T) {
	log := installViewedFakeGH(t, `echo '{"status":"ahead","files":[{"filename":"changed.go"}]}'`)
	cj := session.CritJSON{ViewedFiles: map[string]session.ViewedFile{
		"changed.go": {Viewed: true, HeadSHA: "old", ChangeNumber: 7},
		"same.go":    {Viewed: true, HeadSHA: "old", ChangeNumber: 7},
		"current.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7},
	}}
	remote := prViewedState{headSHA: "head", files: map[string]string{
		"changed.go": "DISMISSED", "same.go": "UNVIEWED", "current.go": "UNVIEWED",
	}}
	got := buildViewedPushPlan(forge.ChangeID{Number: 7}, cj, remote)
	want := viewedPushPlan{mark: []string{"current.go", "same.go"}, stale: []string{"changed.go"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	data, _ := os.ReadFile(log)
	if n := strings.Count(string(data), "compare/"); n != 1 {
		t.Fatalf("compared %d times:\n%s", n, data)
	}
}

func TestRunViewedMutation_Batches(t *testing.T) {
	log := installViewedFakeGH(t, `echo '{"data":{"m0":{"clientMutationId":null}}}'`)
	paths := make([]string, viewedMutationBatch+1)
	for i := range paths {
		paths[i] = "f.go"
	}
	if _, err := runViewedMutation("markFileAsViewed", "PR_1", paths); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if n := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; n != 2 {
		t.Fatalf("got %d requests, want 2:\n%s", n, data)
	}
}

func TestRunViewedMutation_CountsPartialFailure(t *testing.T) {
	installViewedFakeGH(t, `echo '{"data":{"m0":{"clientMutationId":null},"m1":null}}'; exit 1`)
	done, err := runViewedMutation("markFileAsViewed", "PR_1", []string{"a.go", "b.go"})
	if err == nil || done != 1 {
		t.Fatalf("done = %d, err = %v", done, err)
	}
}

func TestViewedFlags(t *testing.T) {
	push, err := parsePushFlags([]string{"--viewed", "7"})
	if err != nil || !push.viewed || push.spec != "7" {
		t.Fatalf("push: %+v, %v", push, err)
	}
	pull, err := parsePullFlags([]string{"--viewed", "7"})
	if err != nil || !pull.viewed || pull.spec != "7" {
		t.Fatalf("pull: %+v, %v", pull, err)
	}
	pushArgs, err := githubPushArgs(forge.PushRequest{Viewed: true, ChangeSpec: "7"})
	if err != nil || !reflect.DeepEqual(pushArgs, []string{"--viewed", "7"}) {
		t.Fatalf("push args: %v, %v", pushArgs, err)
	}
	pullArgs, err := githubPullArgs(forge.PullRequest{Viewed: true, ChangeSpec: "7"})
	if err != nil || !reflect.DeepEqual(pullArgs, []string{"--viewed", "7"}) {
		t.Fatalf("pull args: %v, %v", pullArgs, err)
	}
}
