package gitlab

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

// ParseMRSpec accepts an IID or a canonical GitLab merge-request URL. Nested
// groups are preserved in ChangeID.Project for self-managed/cross-project use.
func ParseMRSpec(spec string) (forge.ChangeID, error) {
	if n, err := strconv.Atoi(spec); err == nil && n > 0 {
		return forge.ChangeID{Number: n}, nil
	}
	u, err := url.Parse(spec)
	if err != nil || u.Hostname() == "" {
		return forge.ChangeID{}, invalidMRSpec(spec)
	}
	marker := "/-/merge_requests/"
	i := strings.Index(u.Path, marker)
	if i <= 0 {
		return forge.ChangeID{}, invalidMRSpec(spec)
	}
	numPart := strings.Trim(strings.SplitN(u.Path[i+len(marker):], "/", 2)[0], "/")
	n, err := strconv.Atoi(numPart)
	if err != nil || n <= 0 {
		return forge.ChangeID{}, invalidMRSpec(spec)
	}
	return forge.ChangeID{Number: n, Project: strings.Trim(u.Path[:i], "/"), Host: u.Host}, nil
}

func invalidMRSpec(spec string) error {
	return fmt.Errorf("invalid merge request %q (expected IID or https://host/group/project/-/merge_requests/N URL)", spec)
}
