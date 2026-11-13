package github

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
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
		{name: "not pinned to a revision", entry: session.ViewedFile{Viewed: true}, remote: "UNVIEWED", stale: never},
		{name: "file not in pr", entry: mark(true), stale: never},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := prViewedState{headSHA: head, files: map[string]string{}}
			if tt.remote != "" {
				remote.files["a.go"] = tt.remote
			}
			got := planViewedPush(map[string]session.ViewedFile{"a.go": tt.entry}, remote, tt.stale)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMergeRemoteViewed_ReplacesTickFromAnotherRevision(t *testing.T) {
	tests := []struct {
		name  string
		entry session.ViewedFile
		want  int
	}{
		{"old head", session.ViewedFile{Viewed: true, HeadSHA: "old", ChangeNumber: 7}, 1},
		{"not pinned", session.ViewedFile{Viewed: true}, 1},
		{"current", session.ViewedFile{Viewed: true, HeadSHA: "head", ChangeNumber: 7}, 0},
		{"untick on old head", session.ViewedFile{HeadSHA: "old", ChangeNumber: 7}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
				"pr:7": {"a.go": tt.entry},
			}}
			remote := prViewedState{headSHA: "head", files: map[string]string{"a.go": "VIEWED"}}
			if got := mergeRemoteViewed(&cj, forge.ChangeID{Number: 7}, remote, "", ""); got != tt.want {
				t.Fatalf("added %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMergeRemoteViewed(t *testing.T) {
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:7": {"unticked.go": {Viewed: false}},
	}}
	remote := prViewedState{headSHA: "head", files: map[string]string{
		"new.go":       "VIEWED",
		"unticked.go":  "VIEWED",
		"dismissed.go": "DISMISSED",
		"unviewed.go":  "UNVIEWED",
	}}
	if got := mergeRemoteViewed(&cj, forge.ChangeID{Number: 7}, remote, "", ""); got != 1 {
		t.Fatalf("added %d, want 1", got)
	}
	marks := cj.ChangeViewedFiles["pr:7"]
	want := session.ViewedFile{Viewed: true, HeadSHA: "head", ChangeNumber: 7}
	if marks["new.go"] != want {
		t.Fatalf("got %+v", marks["new.go"])
	}
	if marks["unticked.go"].Viewed {
		t.Fatal("pull overrode a local untick")
	}
	if len(marks) != 2 || len(cj.ViewedFiles) != 0 {
		t.Fatalf("only VIEWED files are added, under the PR: %+v", cj)
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
			paths, ok := compareChangedFiles(forge.ChangeID{Number: 7, Project: "acme/widget"}, "old", "new")
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
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{"pr:acme/widget#7": {
		"changed.go": {Viewed: true, HeadSHA: "old", ChangeNumber: 7},
		"same.go":    {Viewed: true, HeadSHA: "old", ChangeNumber: 7},
		"current.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7},
	}}}
	remote := prViewedState{headSHA: "head", files: map[string]string{
		"changed.go": "DISMISSED", "same.go": "UNVIEWED", "current.go": "UNVIEWED",
	}}
	got := buildViewedPushPlan(forge.ChangeID{Number: 7, Project: "acme/widget"}, cj, remote)
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

const viewedCLIFakeGH = `case "$*" in
  "auth status"*) exit 0 ;;
  version*) echo "gh version 2.60.0 (2024-01-01)" ;;
  "repo view"*) echo '{"owner":{"login":"acme"},"name":"widget"}' ;;
  *markFileAsViewed*) echo '{"data":{"m0":{"clientMutationId":null}}}' ;;
  *viewerViewedState*) echo '{"data":{"repository":{"pullRequest":{"id":"PR_1","headRefOid":"head","files":{"pageInfo":{"hasNextPage":false},"nodes":[{"path":"local.go","viewerViewedState":"UNVIEWED"},{"path":"remote.go","viewerViewedState":"VIEWED"}]}}}}}' ;;
  *reviewThreads*) echo '{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}' ;;
  api\ repos/*/pulls/*) echo '[[]]' ;;
  *) echo '{}' ;;
esac`

// viewedOnlyReview saves a review holding one mark and no comments.
func viewedOnlyReview(t *testing.T) (dir, critPath string) {
	t.Helper()
	dir = t.TempDir()
	t.Chdir(dir)
	critPath, err := review.ResolveCommandReviewPathWithSession("", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	cj := session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}, ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:7": {"local.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7}},
	}}
	if err := session.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	return dir, critPath
}

func TestRunPush_ViewedOnlyReview(t *testing.T) {
	log := installViewedFakeGH(t, viewedCLIFakeGH)
	dir, _ := viewedOnlyReview(t)
	if err := RunPush([]string{"--viewed", "--output", dir, "7"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "markFileAsViewed") || !strings.Contains(string(data), "p0=local.go") {
		t.Fatalf("mark not pushed:\n%s", data)
	}
}

func TestRunPush_WithoutViewedFlagLeavesGitHubAlone(t *testing.T) {
	log := installViewedFakeGH(t, viewedCLIFakeGH)
	dir, _ := viewedOnlyReview(t)
	if err := RunPush([]string{"--output", dir, "7"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "ViewedState") || strings.Contains(string(data), "markFileAsViewed") {
		t.Fatalf("viewed sync must be opt-in:\n%s", data)
	}
}

func TestRunPush_ViewedDryRunDoesNotMutate(t *testing.T) {
	log := installViewedFakeGH(t, viewedCLIFakeGH)
	dir, _ := viewedOnlyReview(t)
	if err := RunPush([]string{"--viewed", "--dry-run", "--output", dir, "7"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "markFileAsViewed") {
		t.Fatalf("dry run mutated GitHub:\n%s", data)
	}
}

func TestRunPull_ViewedSavesWithoutComments(t *testing.T) {
	installViewedFakeGH(t, viewedCLIFakeGH)
	dir, critPath := viewedOnlyReview(t)
	if err := RunPull([]string{"--viewed", "--output", dir, "7"}); err != nil {
		t.Fatal(err)
	}
	cj := readCritJSONForPull(critPath)
	want := session.ViewedFile{Viewed: true, HeadSHA: "head", ChangeNumber: 7}
	if cj.ChangeViewedFiles["pr:7"]["remote.go"] != want {
		t.Fatalf("got %+v", cj.ChangeViewedFiles)
	}
}

// stubCheckoutPR makes the checkout belong to project on github.com.
func stubCheckoutPR(t *testing.T, project string) *int {
	t.Helper()
	calls := 0
	orig := checkoutPRKey
	checkoutPRKey = func(number int) string {
		calls++
		if project == "" {
			return ""
		}
		return session.PRFocusKey(number, project, "github.com")
	}
	t.Cleanup(func() { checkoutPRKey = orig })
	return &calls
}

func TestViewedMarksFor_SameNumberAcrossRepositories(t *testing.T) {
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:7":                    {"bare.go": {Viewed: true, HeadSHA: "h", ChangeNumber: 7}},
		"pr:acme/widget#7":        {"widget.go": {Viewed: true, HeadSHA: "h", ChangeNumber: 7}},
		"pr:other/thing#7":        {"thing.go": {Viewed: true, HeadSHA: "h", ChangeNumber: 7}},
		"pr:ghe.io/acme/widget#7": {"ghe.go": {Viewed: true, HeadSHA: "h", ChangeNumber: 7}},
		"pr:acme/widget#8":        {"eight.go": {Viewed: true, HeadSHA: "h", ChangeNumber: 8}},
	}}
	tests := []struct {
		name     string
		checkout string // repository of this checkout; "" = unknown
		id       forge.ChangeID
		want     []string
	}{
		{"url for the checkout repo", "acme/widget", forge.ChangeID{Number: 7, Project: "acme/widget"}, []string{"bare.go", "widget.go"}},
		{"bare number in the checkout", "acme/widget", forge.ChangeID{Number: 7}, []string{"bare.go", "widget.go"}},
		{"url for another repo", "acme/widget", forge.ChangeID{Number: 7, Project: "other/thing"}, []string{"thing.go"}},
		{"owner case differs", "acme/widget", forge.ChangeID{Number: 7, Project: "Other/Thing"}, []string{"thing.go"}},
		{"same repo name on another host", "acme/widget", forge.ChangeID{Number: 7, Project: "acme/widget", Host: "ghe.io"}, []string{"ghe.go"}},
		{"checkout unknown, bare", "", forge.ChangeID{Number: 7}, []string{"bare.go"}},
		{"checkout unknown, url", "", forge.ChangeID{Number: 7, Project: "acme/widget"}, []string{"widget.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubCheckoutPR(t, tt.checkout)
			got := viewedMarksFor(cj, viewedChangeKeys(cj, tt.id, "", func() string { return checkoutPRKey(tt.id.Number) }))
			var paths []string
			for p := range got {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			if !reflect.DeepEqual(paths, tt.want) {
				t.Fatalf("got %v, want %v", paths, tt.want)
			}
		})
	}
}

func TestViewedChangeKeys_ResolvesCheckoutOnlyWhenNeeded(t *testing.T) {
	calls := stubCheckoutPR(t, "acme/widget")
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:7": {"a.go": {Viewed: true}},
	}}
	resolve := func() string { return checkoutPRKey(7) }
	viewedChangeKeys(cj, forge.ChangeID{Number: 7}, "", resolve)
	if *calls != 0 {
		t.Fatalf("resolved the checkout %d times with only the primary key present", *calls)
	}
	// A GitLab MR with the same number is not this PR under another key.
	cj.ChangeViewedFiles["mr:acme/widget#7"] = map[string]session.ViewedFile{"b.go": {Viewed: true}}
	viewedChangeKeys(cj, forge.ChangeID{Number: 7}, "", resolve)
	if *calls != 0 {
		t.Fatalf("an MR key made the checkout resolve %d times", *calls)
	}
}

func TestBuildViewedPushPlan_IgnoresSameNumberInAnotherRepo(t *testing.T) {
	stubCheckoutPR(t, "acme/widget")
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:other/thing#7": {"a.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7}},
	}}
	remote := prViewedState{headSHA: "head", files: map[string]string{"a.go": "UNVIEWED"}}
	if got := buildViewedPushPlan(forge.ChangeID{Number: 7}, cj, remote); len(got.mark) != 0 {
		t.Fatalf("pushed another repository's mark: %+v", got)
	}
	got := buildViewedPushPlan(forge.ChangeID{Number: 7, Project: "other/thing"}, cj, remote)
	if !reflect.DeepEqual(got.mark, []string{"a.go"}) {
		t.Fatalf("mark for its own PR not pushed: %+v", got)
	}
}

func TestMergeRemoteViewed_KeepsLayersApart(t *testing.T) {
	stubCheckoutPR(t, "acme/widget")
	// Layer 8 unticked a.go; pulling layer 7 must not touch that mark.
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:8": {"a.go": {Viewed: false, HeadSHA: "h8", ChangeNumber: 8}},
	}}
	remote := prViewedState{headSHA: "h7", files: map[string]string{"a.go": "VIEWED"}}
	if got := mergeRemoteViewed(&cj, forge.ChangeID{Number: 7}, remote, "pr:8", "pr:acme/widget#7"); got != 1 {
		t.Fatalf("added %d, want 1", got)
	}
	if cj.ChangeViewedFiles["pr:8"]["a.go"].Viewed {
		t.Fatal("pull of PR 7 changed PR 8's mark")
	}
	if !cj.ChangeViewedFiles["pr:7"]["a.go"].Viewed {
		t.Fatalf("PR 7 mark missing: %+v", cj.ChangeViewedFiles)
	}
}

func TestMergeRemoteViewed_UsesExistingBucketForCheckoutRepo(t *testing.T) {
	stubCheckoutPR(t, "acme/widget")
	// The daemon was started with a bare --pr 7, so its marks live under pr:7.
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:7": {"b.go": {Viewed: true, HeadSHA: "head", ChangeNumber: 7}},
	}}
	remote := prViewedState{headSHA: "head", files: map[string]string{"a.go": "VIEWED"}}
	mergeRemoteViewed(&cj, forge.ChangeID{Number: 7, Project: "acme/widget"}, remote, "", "pr:acme/widget#7")
	if _, ok := cj.ChangeViewedFiles["pr:7"]["a.go"]; !ok || len(cj.ChangeViewedFiles) != 1 {
		t.Fatalf("pulled mark not added beside the daemon's: %+v", cj.ChangeViewedFiles)
	}
}

func TestMergeRemoteViewed_FirstPullUsesTheDaemonsBucket(t *testing.T) {
	remote := prViewedState{headSHA: "head", files: map[string]string{"a.go": "VIEWED"}}
	tests := []struct {
		name            string
		id              forge.ChangeID
		daemonKey, want string
		checkoutKey     string
	}{
		{"daemon from url, bare pull", forge.ChangeID{Number: 7}, "pr:acme/widget#7", "pr:acme/widget#7", "pr:acme/widget#7"},
		{"bare daemon, url pull", forge.ChangeID{Number: 7, Project: "acme/widget"}, "pr:7", "pr:7", "pr:acme/widget#7"},
		{"daemon on another repo's pr 7", forge.ChangeID{Number: 7}, "pr:other/thing#7", "pr:7", "pr:acme/widget#7"},
		{"no daemon", forge.ChangeID{Number: 7}, "", "pr:7", "pr:acme/widget#7"},
		{"checkout unknown", forge.ChangeID{Number: 7}, "pr:acme/widget#7", "pr:7", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cj session.CritJSON
			mergeRemoteViewed(&cj, tt.id, remote, tt.daemonKey, tt.checkoutKey)
			if !cj.ChangeViewedFiles[tt.want]["a.go"].Viewed || len(cj.ChangeViewedFiles) != 1 {
				t.Fatalf("want the mark under %s: %+v", tt.want, cj.ChangeViewedFiles)
			}
		})
	}
}

func TestViewedMarksFor_CaseVariantsAreDeterministic(t *testing.T) {
	cj := session.CritJSON{ChangeViewedFiles: map[string]map[string]session.ViewedFile{
		"pr:Acme/Widget#7": {"a.go": {Viewed: false}},
		"pr:ACME/widget#7": {"a.go": {Viewed: false}},
		"pr:acme/widget#7": {"a.go": {Viewed: true}},
	}}
	for i := 0; i < 50; i++ {
		if !viewedMarksFor(cj, []string{"pr:acme/widget#7"})["a.go"].Viewed {
			t.Fatal("the exact-case bucket must win")
		}
		if viewedMarksFor(cj, []string{"pr:aCME/WIDGET#7"})["a.go"].Viewed {
			t.Fatal("folded matches must be read in sorted order")
		}
	}
}

func TestCheckoutPRKey(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"github.com", `echo '{"nameWithOwner":"acme/widget","url":"https://github.com/acme/widget"}'`, "pr:acme/widget#7"},
		{"enterprise host", `echo '{"nameWithOwner":"acme/widget","url":"https://GHE.example.com/acme/widget"}'`, "pr:ghe.example.com/acme/widget#7"},
		{"not a repo", `exit 1`, ""},
		{"unparseable", `echo 'nope'`, ""},
		{"no name", `echo '{"url":"https://github.com/acme/widget"}'`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installViewedFakeGH(t, tt.body)
			if got := checkoutPRKey(7); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
