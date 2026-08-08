package forge

import (
	"reflect"
	"testing"
)

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
