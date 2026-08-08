package forge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type cliTestProvider struct {
	kind        Kind
	pullRequest PullRequest
	pushRequest PushRequest
	pullErr     error
	pushErr     error
}

func (p *cliTestProvider) Kind() Kind                                   { return p.kind }
func (*cliTestProvider) RequireAuth(context.Context, RepoContext) error { return nil }
func (*cliTestProvider) Detect(context.Context, RepoContext) (ChangeID, error) {
	return ChangeID{}, nil
}
func (*cliTestProvider) Get(context.Context, RepoContext, ChangeID) (ChangeRequest, error) {
	return ChangeRequest{}, nil
}
func (*cliTestProvider) ListOpen(context.Context, RepoContext) ([]ChangeSummary, error) {
	return nil, nil
}
func (p *cliTestProvider) Pull(_ context.Context, request PullRequest) (PullResult, error) {
	p.pullRequest = request
	return PullResult{Imported: 2}, p.pullErr
}
func (p *cliTestProvider) Push(_ context.Context, request PushRequest) (PushResult, error) {
	p.pushRequest = request
	return PushResult{Created: 1}, p.pushErr
}
func (*cliTestProvider) FetchFile(context.Context, RepoContext, RepoRef, string, string) ([]byte, error) {
	return nil, nil
}
func (*cliTestProvider) Invalidate(ChangeID) {}

func withCLIProvider(t *testing.T, provider *cliTestProvider) *Kind {
	t.Helper()
	oldSelect := SelectProviderFn
	var selected Kind
	SelectProviderFn = func(kind Kind) (Provider, error) {
		selected = kind
		return provider, nil
	}
	t.Cleanup(func() { SelectProviderFn = oldSelect })
	return &selected
}

func TestExtractKindFlag(t *testing.T) {
	kind, args, err := extractKindFlag([]string{"--dry-run", "--forge", "gitlab", "42"})
	if err != nil {
		t.Fatal(err)
	}
	if kind != GitLab || !reflect.DeepEqual(args, []string{"--dry-run", "42"}) {
		t.Fatalf("extractKindFlag = (%q, %#v)", kind, args)
	}
}

func TestParsePullRequest(t *testing.T) {
	request, err := parsePullRequest([]string{"--output", "reviews", "https://gitlab.example/a/b/-/merge_requests/9"})
	if err != nil {
		t.Fatal(err)
	}
	if request.OutputDir != "reviews" || request.ChangeSpec == "" {
		t.Fatalf("request = %+v", request)
	}
}

func TestParsePushRequest(t *testing.T) {
	request, err := parsePushRequest([]string{"--dry-run", "--event", "request-changes", "-m", "please fix", "42"})
	if err != nil {
		t.Fatal(err)
	}
	if !request.DryRun || request.Event != "request-changes" || request.Message != "please fix" || request.ChangeSpec != "42" {
		t.Fatalf("request = %+v", request)
	}
}

func TestParsePushRequestRequiresChangeMessage(t *testing.T) {
	if _, err := parsePushRequest([]string{"--event", "request-changes", "42"}); err == nil {
		t.Fatal("expected request-changes message validation")
	}
}

func TestArgsContainMRURL(t *testing.T) {
	if !argsContainMRURL([]string{"https://gitlab.example.com/a/b/-/merge_requests/4"}) {
		t.Fatal("MR URL not detected")
	}
	if argsContainMRURL([]string{"https://github.com/a/b/pull/4"}) {
		t.Fatal("GitHub URL detected as MR")
	}
}

func TestRunPullSelectsGitLabFromURLAndDispatches(t *testing.T) {
	provider := &cliTestProvider{kind: GitLab}
	selected := withCLIProvider(t, provider)
	url := "https://gitlab.example/acme/widget/-/merge_requests/9"
	if err := RunPull([]string{"--output", "reviews", url}); err != nil {
		t.Fatal(err)
	}
	if *selected != GitLab {
		t.Fatalf("selected provider = %q, want gitlab", *selected)
	}
	if provider.pullRequest.OutputDir != "reviews" || provider.pullRequest.ChangeSpec != url {
		t.Fatalf("pull request = %+v", provider.pullRequest)
	}
}

func TestRunPushSelectsExplicitProviderAndDispatches(t *testing.T) {
	provider := &cliTestProvider{kind: GitHub}
	selected := withCLIProvider(t, provider)
	if err := RunPush([]string{"--forge=github", "--dry-run", "--event", "approve", "--message", "ship it", "42"}); err != nil {
		t.Fatal(err)
	}
	if *selected != GitHub {
		t.Fatalf("selected provider = %q, want github", *selected)
	}
	want := PushRequest{ChangeSpec: "42", DryRun: true, Event: "approve", Message: "ship it"}
	if !reflect.DeepEqual(provider.pushRequest, want) {
		t.Fatalf("push request = %+v, want %+v", provider.pushRequest, want)
	}
}

func TestRunPullAndPushReturnProviderErrors(t *testing.T) {
	pullErr := errors.New("pull failed")
	provider := &cliTestProvider{pullErr: pullErr}
	withCLIProvider(t, provider)
	if err := RunPull([]string{"1"}); !errors.Is(err, pullErr) {
		t.Fatalf("RunPull error = %v", err)
	}

	pushErr := errors.New("push failed")
	provider.pushErr = pushErr
	if err := RunPush([]string{"1"}); !errors.Is(err, pushErr) {
		t.Fatalf("RunPush error = %v", err)
	}
}

func TestRunChangeRoutesPRAndMR(t *testing.T) {
	oldReview := ReviewFn
	defer func() { ReviewFn = oldReview }()
	var calls [][]string
	ReviewFn = func(args []string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	if err := RunChange(GitHub, []string{"12"}); err != nil {
		t.Fatal(err)
	}
	if err := RunChange(GitLab, []string{"https://gitlab.example/a/b/-/merge_requests/7"}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"--pr", "12"}, {"--mr", "https://gitlab.example/a/b/-/merge_requests/7"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("review calls = %#v, want %#v", calls, want)
	}
}

func TestRunChangeAndSelectionErrors(t *testing.T) {
	oldReview, oldSelect := ReviewFn, SelectProviderFn
	t.Cleanup(func() {
		ReviewFn = oldReview
		SelectProviderFn = oldSelect
	})
	ReviewFn = nil
	if err := RunChange(GitLab, []string{"7"}); err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("unwired review error = %v", err)
	}
	if err := RunChange(GitHub, nil); err == nil || !strings.Contains(err.Error(), "usage: crit pr") {
		t.Fatalf("invalid PR args error = %v", err)
	}
	SelectProviderFn = nil
	if err := RunPull([]string{"1"}); err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("unwired provider error = %v", err)
	}
	if err := RunPush([]string{"--forge", "unknown", "1"}); err == nil {
		t.Fatal("unknown forge unexpectedly accepted")
	}
}

func TestCLIParsingRejectsMissingAndDuplicateValues(t *testing.T) {
	for _, args := range [][]string{{"--forge"}} {
		if _, _, err := extractKindFlag(args); err == nil {
			t.Errorf("extractKindFlag(%v) unexpectedly succeeded", args)
		}
	}
	for _, args := range [][]string{{"--output"}, {"1", "2"}} {
		if _, err := parsePullRequest(args); err == nil {
			t.Errorf("parsePullRequest(%v) unexpectedly succeeded", args)
		}
	}
	for _, args := range [][]string{{"--message"}, {"--output"}, {"--event"}, {"1", "2"}, {"--event", "bogus"}} {
		if _, err := parsePushRequest(args); err == nil {
			t.Errorf("parsePushRequest(%v) unexpectedly succeeded", args)
		}
	}
}
