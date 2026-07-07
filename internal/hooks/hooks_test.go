package hooks_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/hooks"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func TestFilenames(t *testing.T) {
	got := hooks.Filenames(prompt.HookFinishUnresolved, "diff")
	want := []string{"on_finish_unresolved.diff.sh", "on_finish_unresolved.sh"}
	if len(got) != len(want) {
		t.Fatalf("filenames = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("filenames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadCommand_Inline(t *testing.T) {
	ec, err := hooks.LoadCommand("inline:echo hi", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if ec.Kind != "inline" || ec.Arg != "echo hi" {
		t.Fatalf("ec = %+v", ec)
	}
}

func TestLoadCommand_FileRelative(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "myhook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ec, err := hooks.LoadCommand("file:myhook.sh", dir)
	if err != nil {
		t.Fatal(err)
	}
	if ec.Kind != "file" || ec.Arg != script {
		t.Fatalf("ec = %+v want %q", ec, script)
	}
}

func TestLoadCommand_BadPrefix(t *testing.T) {
	if _, err := hooks.LoadCommand("echo hi", "/tmp"); err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

func TestLoadCommand_FileMissing(t *testing.T) {
	if _, err := hooks.LoadCommand("file:nope.sh", t.TempDir()); err == nil {
		t.Fatal("expected error for missing script")
	}
}

func TestResolveFinishCommand_GlobalConfig(t *testing.T) {
	home := t.TempDir()
	global := map[string]string{"on_finish_approved": "inline:echo approved"}
	ec, err := hooks.ResolveFinishCommand(global, nil, "/unused", home, prompt.HookFinishApproved, "diff", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Kind != "inline" || ec.Arg != "echo approved" {
		t.Fatalf("ec = %+v", ec)
	}
	if ec.Hook != "on_finish_approved" {
		t.Fatalf("hook = %q", ec.Hook)
	}
	if ec.Source != "global:inline" {
		t.Fatalf("source = %q", ec.Source)
	}
}

func TestResolveFinishCommand_ProjectOverridesGlobal(t *testing.T) {
	global := map[string]string{"on_finish_approved": "inline:global"}
	project := map[string]string{"on_finish_approved": "inline:project"}
	ec, err := hooks.ResolveFinishCommand(global, project, "/unused", t.TempDir(), prompt.HookFinishApproved, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Arg != "project" || ec.Layer != hooks.LayerProject {
		t.Fatalf("ec = %+v", ec)
	}
}

func TestResolveFinishCommand_ModeSpecificKey(t *testing.T) {
	global := map[string]string{
		"on_finish_unresolved":      "inline:generic",
		"on_finish_unresolved:diff": "inline:diff",
	}
	ec, err := hooks.ResolveFinishCommand(global, nil, "", "", prompt.HookFinishUnresolved, "diff", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Arg != "diff" || ec.Hook != "on_finish_unresolved:diff" {
		t.Fatalf("ec = %+v", ec)
	}
}

func TestResolveFinishCommand_DiscoveredFileProject(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, ".crit", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(hooksDir, "on_finish_approved.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ec, err := hooks.ResolveFinishCommand(nil, nil, dir, t.TempDir(), prompt.HookFinishApproved, "files", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec == nil || ec.Kind != "file" || ec.Arg != script {
		t.Fatalf("ec = %+v", ec)
	}
	if ec.Source != "project:.crit/hooks/on_finish_approved.sh" {
		t.Fatalf("source = %q", ec.Source)
	}
}

func TestResolveFinishCommand_ProjectBlockedWhenUntrusted(t *testing.T) {
	project := map[string]string{"on_finish_approved": "inline:project"}
	ec, err := hooks.ResolveFinishCommand(nil, project, "/unused", t.TempDir(), prompt.HookFinishApproved, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if ec != nil {
		t.Fatalf("expected nil when project hook present but useProject=false, got %+v", ec)
	}
}

func TestResolveFinishCommand_None(t *testing.T) {
	ec, err := hooks.ResolveFinishCommand(nil, nil, t.TempDir(), t.TempDir(), prompt.HookFinishApproved, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if ec != nil {
		t.Fatalf("expected nil, got %+v", ec)
	}
}

func TestRun_InlineWritesStdoutStdinEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inline sh -c hook tested on unix")
	}
	ec := hooks.ExecutableCommand{Kind: "inline", Arg: "printf %s \"$CRIT_SESSION_KEY\" > \"$HOOK_OUT\"; cat; echo -$CRIT_MODE"}
	out := t.TempDir()
	marker := filepath.Join(out, "marker.txt")
	in := hooks.Input{
		Stdin: []byte("STDINPAYLOAD"),
		Env: map[string]string{
			"CRIT_SESSION_KEY": "sk123",
			"CRIT_MODE":        "diff",
			"HOOK_OUT":         marker,
		},
		Timeout: 5 * time.Second,
	}
	res, err := hooks.Run(context.Background(), ec, in)
	if err != nil {
		t.Fatalf("run: %v stderr=%s", err, string(res.Stderr))
	}
	got, _ := os.ReadFile(marker)
	if string(got) != "sk123" {
		t.Fatalf("marker = %q want sk123 (env propagation)", string(got))
	}
	if !strings.Contains(string(res.Stdout), "STDINPAYLOAD-diff") {
		t.Fatalf("stdout = %q want stdin echoed + mode appended", string(res.Stdout))
	}
}

func TestRun_FileHookShebang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file shebang hook tested on unix")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	marker := filepath.Join(dir, "out.txt")
	body := "#!/bin/sh\nprintf %s \"$CRIT_APPROVED\" > \"" + marker + "\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	ec, err := hooks.LoadCommand("file:hook.sh", dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = hooks.Run(context.Background(), ec, hooks.Input{
		Env: map[string]string{"CRIT_APPROVED": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(marker)
	if string(got) != "true" {
		t.Fatalf("marker = %q", string(got))
	}
}

func TestRun_NonZeroExitReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-zero exit hook tested on unix")
	}
	ec := hooks.ExecutableCommand{Kind: "inline", Arg: "exit 3"}
	out, err := hooks.Run(context.Background(), ec, hooks.Input{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected error for exit 3")
	}
	if out == nil || out.ExitCode != 3 {
		t.Fatalf("exitcode = %d", out.ExitCode)
	}
}

func TestRun_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout hook tested on unix")
	}
	ec := hooks.ExecutableCommand{Kind: "inline", Arg: "sleep 5"}
	out, err := hooks.Run(context.Background(), ec, hooks.Input{Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if out != nil && !out.TimedOut {
		t.Fatalf("expected TimedOut=true, err=%v", err)
	}
}

func TestEnvMapAndJSONPayload(t *testing.T) {
	ctx := prompt.Context{
		ReviewPath:        "/tmp/r.json",
		SessionKey:        "sk1",
		Mode:              "diff",
		UnresolvedCount:   3,
		TotalCount:        5,
		Approved:          false,
		FilesWithComments: []string{"a.go", "b.go"},
	}
	env := hooks.EnvMap(ctx)
	if env["CRIT_REVIEW_PATH"] != "/tmp/r.json" || env["CRIT_UNRESOLVED_COUNT"] != "3" || env["CRIT_APPROVED"] != "false" {
		t.Fatalf("env = %+v", env)
	}
	if env["CRIT_FILES_WITH_COMMENTS"] != "a.go\nb.go" {
		t.Fatalf("files env = %q", env["CRIT_FILES_WITH_COMMENTS"])
	}
	payload := string(hooks.JSONPayload(ctx))
	if !strings.Contains(payload, `"unresolved_count": 3`) {
		t.Fatalf("payload missing unresolved_count: %s", payload)
	}
	if !strings.Contains(payload, `"files_with_comments"`) || !strings.Contains(payload, `"a.go"`) || !strings.Contains(payload, `"b.go"`) {
		t.Fatalf("payload missing files_with_comments: %s", payload)
	}
	// empty comments arrays render as [] (not null)
	if !strings.Contains(payload, `"comments_unresolved_json": []`) {
		t.Fatalf("payload missing empty array: %s", payload)
	}
}
