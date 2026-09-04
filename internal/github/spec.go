package github

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

// ParsePRSpec accepts a PR number or a canonical GitHub pull-request URL.
// Bare numbers leave Project/Host empty (resolved against the current
// checkout). Full URLs populate Project as "owner/repo" and Host from the URL
// so cross-repo reviews do not collapse onto the cwd remote (#870).
func ParsePRSpec(spec string) (forge.ChangeID, error) {
	if n, err := strconv.Atoi(spec); err == nil && n > 0 {
		return forge.ChangeID{Number: n}, nil
	}
	u, err := url.Parse(spec)
	if err != nil || u.Hostname() == "" {
		return forge.ChangeID{}, invalidPRSpec(spec)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// owner/repo/pull/N — optionally followed by tab suffixes (files, commits, …).
	if len(parts) < 4 || parts[2] != "pull" {
		return forge.ChangeID{}, invalidPRSpec(spec)
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return forge.ChangeID{}, invalidPRSpec(spec)
	}
	return forge.ChangeID{
		Number:  n,
		Project: parts[0] + "/" + parts[1],
		Host:    normalizeGitHubHost(u.Host),
	}, nil
}

func invalidPRSpec(spec string) error {
	return fmt.Errorf("invalid --pr value %q (expected number or https://host/owner/repo/pull/N URL)", spec)
}

// normalizeGitHubHost collapses github.com aliases so cache/session keys and
// -R flags stay consistent (www., ports, case).
func normalizeGitHubHost(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.EqualFold(host, "github.com") || strings.EqualFold(host, "www.github.com") {
		return "github.com"
	}
	return strings.ToLower(host)
}

// prRepoRef is the -R / cache-key repository identity for id.
func prRepoRef(id forge.ChangeID) string {
	if id.Project == "" {
		return ""
	}
	if id.Host != "" && !strings.EqualFold(id.Host, "github.com") {
		return id.Host + "/" + id.Project
	}
	return id.Project
}

// prViewArgs builds the `gh pr view` argument list for id. When Project is
// set (from a full PR URL), pin the lookup with -R so the current checkout's
// remote cannot shadow a different owner/repo (#870). Non-github.com hosts
// are included as HOST/OWNER/REPO so GitHub Enterprise URLs resolve correctly.
func prViewArgs(id forge.ChangeID) []string {
	args := []string{"pr", "view", strconv.Itoa(id.Number)}
	if repo := prRepoRef(id); repo != "" {
		args = append(args, "-R", repo)
	}
	return append(args, "--json", prJSONFields)
}

// prCacheKey namespaces PR metadata by host/owner/repo when known so same-number
// PRs in different repos (or hosts) do not collide in the daemon cache.
func prCacheKey(id forge.ChangeID) string {
	if repo := prRepoRef(id); repo != "" {
		return repo + "#" + strconv.Itoa(id.Number)
	}
	return strconv.Itoa(id.Number)
}
