package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

var commandContext = exec.CommandContext

func requireGLab(ctx context.Context, repo forge.RepoContext) error {
	if _, err := exec.LookPath("glab"); err != nil {
		return fmt.Errorf("glab CLI not found. Install it: https://gitlab.com/gitlab-org/cli")
	}
	args := []string{"auth", "status"}
	if repo.Host != "" {
		args = append(args, "--hostname", repo.Host)
	}
	cmd := commandContext(ctx, "glab", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("glab is not authenticated%s. Run: glab auth login: %s",
			hostSuffix(repo.Host), strings.TrimSpace(string(out)))
	}
	return nil
}

func hostSuffix(host string) string {
	if host == "" {
		return ""
	}
	return " for " + host
}

func runAPI(ctx context.Context, host, endpoint, method string, payload any) ([]byte, error) {
	args := []string{"api"}
	if method != "" && method != "GET" {
		args = append(args, "--method", method)
	}
	if host != "" {
		args = append(args, "--hostname", host)
	}
	args = append(args, endpoint)
	var input []byte
	if payload != nil {
		var err error
		input, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal GitLab API payload: %w", err)
		}
		args = append(args, "--header", "Content-Type: application/json", "--input", "-")
	}
	cmd := commandContext(ctx, "glab", args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("glab api %s: %s: %w", endpoint, strings.TrimSpace(string(out)), err)
	}
	return out, nil
}

func projectEndpoint(id forge.ChangeID, suffix string) string {
	project := ":fullpath"
	if id.Project != "" {
		project = url.PathEscape(id.Project)
	}
	return fmt.Sprintf("projects/%s/merge_requests/%d%s", project, id.Number, suffix)
}
