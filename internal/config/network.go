package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	// AllowUnauthenticatedNetworkFlag is the CLI flag that acknowledges
	// unauthenticated network exposure (non-loopback listen or public_url).
	AllowUnauthenticatedNetworkFlag = "allow-unauthenticated-network"
	// AllowUnauthenticatedNetworkEnv is the env var equivalent of the flag.
	AllowUnauthenticatedNetworkEnv = "CRIT_ALLOW_UNAUTHENTICATED_NETWORK"
)

// IsLoopbackHost reports whether host (no port) is a loopback address.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NeedsUnauthenticatedNetworkAck reports whether the effective listen host or
// advertised public URL implies network exposure of the unauthenticated API.
func NeedsUnauthenticatedNetworkAck(host, publicURL string) bool {
	if strings.TrimSpace(publicURL) != "" {
		return true
	}
	if host == "" {
		return false
	}
	return !IsLoopbackHost(host)
}

// EnvAllowsUnauthenticatedNetwork reports whether the escape-hatch env var is set.
func EnvAllowsUnauthenticatedNetwork() bool {
	v := strings.TrimSpace(os.Getenv(AllowUnauthenticatedNetworkEnv))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ErrUnauthenticatedNetwork returns a user-facing error when network exposure
// was requested without the explicit escape hatch.
func ErrUnauthenticatedNetwork(host, publicURL string) error {
	reason := fmt.Sprintf("listen host %q", host)
	if strings.TrimSpace(publicURL) != "" {
		reason = fmt.Sprintf("public URL %q", publicURL)
		if !IsLoopbackHost(host) {
			reason = fmt.Sprintf("listen host %q and public URL %q", host, publicURL)
		}
	}
	return fmt.Errorf(
		"refusing unauthenticated network exposure (%s).\n\n"+
			"  Crit has no network authentication. Anyone who can reach the port can\n"+
			"  read review/repo files and write comments (including agent-triggering ones).\n\n"+
			"  Prefer keeping the listen host on loopback and reaching Crit via:\n"+
			"    • SSH local forward:  ssh -L 8080:127.0.0.1:8080 …\n"+
			"    • Tailscale Serve / a reverse proxy to 127.0.0.1\n"+
			"    • Docker:  -p 127.0.0.1:8080:8080\n\n"+
			"  To proceed deliberately on a trusted network, pass:\n"+
			"    --%s\n"+
			"  or set %s=1",
		reason,
		AllowUnauthenticatedNetworkFlag,
		AllowUnauthenticatedNetworkEnv,
	)
}
