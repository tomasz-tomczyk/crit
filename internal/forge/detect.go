package forge

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseKind validates a configured or CLI-supplied forge value.
func ParseKind(value string) (Kind, error) {
	switch Kind(strings.ToLower(strings.TrimSpace(value))) {
	case "", Auto:
		return Auto, nil
	case GitHub:
		return GitHub, nil
	case GitLab:
		return GitLab, nil
	default:
		return "", fmt.Errorf("invalid forge %q (expected auto, github, or gitlab)", value)
	}
}

// DetectKind applies an explicit selection first, then recognizes common
// remote hosts. Unknown hosts retain GitHub as Crit's compatibility default.
func DetectKind(explicit string, remote string) (Kind, error) {
	kind, err := ParseKind(explicit)
	if err != nil || kind != Auto {
		return kind, err
	}
	host := remoteHost(remote)
	if strings.Contains(host, "gitlab") {
		return GitLab, nil
	}
	return GitHub, nil
}

// RemoteHost returns the hostname from HTTPS/SSH/SCP-style remotes.
func RemoteHost(remote string) string { return remoteHost(remote) }

func remoteHost(remote string) string {
	remote = strings.TrimSpace(remote)
	if u, err := url.Parse(remote); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	// SCP-style SSH remote: git@example.com:group/project.git.
	if at := strings.LastIndex(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	if colon := strings.IndexByte(remote, ':'); colon >= 0 {
		return strings.ToLower(remote[:colon])
	}
	return ""
}
