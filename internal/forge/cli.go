package forge

import (
	"context"
	"fmt"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

// SelectProviderFn and ReviewFn are wired by cmd/crit, where concrete
// providers and the session package can be imported without cycles.
var (
	SelectProviderFn func(Kind) (Provider, error)
	ReviewFn         func([]string) error
)

// RunPull parses forge-neutral CLI arguments and dispatches to one provider.
func RunPull(args []string) error {
	kind, cleanArgs, err := extractKindFlag(args)
	if err != nil {
		return err
	}
	if kind == Auto {
		if argsContainMRURL(cleanArgs) {
			kind = GitLab
		} else if argsContainPRURL(cleanArgs) {
			kind = GitHub
		}
	}
	request, err := parsePullRequest(cleanArgs)
	if err != nil {
		return err
	}
	provider, err := selectProvider(kind)
	if err != nil {
		return err
	}
	_, err = provider.Pull(context.Background(), request)
	return err
}

// RunPush parses forge-neutral CLI arguments and dispatches to one provider.
func RunPush(args []string) error {
	kind, cleanArgs, err := extractKindFlag(args)
	if err != nil {
		return err
	}
	if kind == Auto {
		if argsContainMRURL(cleanArgs) {
			kind = GitLab
		} else if argsContainPRURL(cleanArgs) {
			kind = GitHub
		}
	}
	request, err := parsePushRequest(cleanArgs)
	if err != nil {
		return err
	}
	provider, err := selectProvider(kind)
	if err != nil {
		return err
	}
	_, err = provider.Push(context.Background(), request)
	return err
}

// RunChange opens a GitHub PR or GitLab MR review through one neutral entrypoint.
func RunChange(kind Kind, args []string) error {
	if len(args) != 1 {
		noun := "pr"
		identifier := "num"
		if kind == GitLab {
			noun = "mr"
			identifier = "iid"
		}
		return fmt.Errorf("usage: crit %s <%s|url>", noun, identifier)
	}
	if ReviewFn == nil {
		return fmt.Errorf("forge review command is not wired")
	}
	flag := "--pr"
	if kind == GitLab {
		flag = "--mr"
	}
	return ReviewFn([]string{flag, args[0]})
}

func selectProvider(kind Kind) (Provider, error) {
	if SelectProviderFn == nil {
		return nil, fmt.Errorf("forge provider selection is not wired")
	}
	return SelectProviderFn(kind)
}

func extractKindFlag(args []string) (Kind, []string, error) {
	kind := Auto
	clean := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := ""
		switch {
		case arg == "--forge":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--forge requires a value")
			}
			i++
			value = args[i]
		case strings.HasPrefix(arg, "--forge="):
			value = strings.TrimPrefix(arg, "--forge=")
		default:
			clean = append(clean, arg)
			continue
		}
		parsed, err := ParseKind(value)
		if err != nil {
			return "", nil, err
		}
		kind = parsed
	}
	return kind, clean, nil
}

func argsContainMRURL(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/-/merge_requests/") {
			return true
		}
	}
	return false
}

func argsContainPRURL(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/pull/") {
			return true
		}
	}
	return false
}

func parsePullRequest(args []string) (PullRequest, error) {
	var request PullRequest
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--output", "-o":
			val, next, err := requireFlagValue(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.OutputDir = val
		case "--session":
			id, next, err := parseSessionFlag(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.SessionID = id
		default:
			if request.ChangeSpec != "" {
				return request, fmt.Errorf("usage: crit pull [--forge <provider>] [--session <id>] [--output <dir>] [number|url]")
			}
			request.ChangeSpec = args[i]
		}
	}
	return request, nil
}

func parsePushRequest(args []string) (PushRequest, error) {
	request := PushRequest{Event: "comment"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			request.DryRun = true
		case "--message", "-m":
			val, next, err := requireFlagValue(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.Message = val
		case "--output", "-o":
			val, next, err := requireFlagValue(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.OutputDir = val
		case "--session":
			id, next, err := parseSessionFlag(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.SessionID = id
		case "--event", "-e":
			val, next, err := requireFlagValue(args, i)
			if err != nil {
				return request, err
			}
			i = next
			request.Event = strings.ToLower(val)
		default:
			if request.ChangeSpec != "" {
				return request, fmt.Errorf("usage: crit push [--forge <provider>] [--session <id>] [--dry-run] [--event <type>] [--message <msg>] [--output <dir>] [number|url]")
			}
			request.ChangeSpec = args[i]
		}
	}
	switch request.Event {
	case "comment", "approve", "request-changes":
	default:
		return request, fmt.Errorf("invalid --event %q (expected comment, approve, or request-changes)", request.Event)
	}
	if request.Event == "request-changes" && request.Message == "" {
		return request, fmt.Errorf("--event request-changes requires --message")
	}
	return request, nil
}

func requireFlagValue(args []string, i int) (string, int, error) {
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%s requires a value", args[i])
	}
	return args[i+1], i + 1, nil
}

func parseSessionFlag(args []string, i int) (string, int, error) {
	id, next, err := requireFlagValue(args, i)
	if err != nil {
		return "", i, err
	}
	if !daemon.ValidSessionKey(id) {
		return "", i, daemon.InvalidSessionIDError(id)
	}
	return id, next, nil
}
