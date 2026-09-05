package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// TargetAuth contains credentials and cached identity for exactly one Crit
// deployment. A target's canonical URL is its authentication boundary.
type TargetAuth struct {
	Token     string `json:"token,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	UserName  string `json:"user_name,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
}

// ShareTarget is one configured Crit deployment.
type ShareTarget struct {
	Name           string     `json:"name,omitempty"`
	URL            string     `json:"url"`
	Default        bool       `json:"default,omitempty"`
	ProxyAuth      bool       `json:"proxy_auth,omitempty"`
	ShareConsented bool       `json:"share_consented,omitempty"`
	Auth           TargetAuth `json:"auth,omitempty"`
}

// CrossOriginRedirectError is returned before an authenticated client follows
// a redirect to a different origin.
type CrossOriginRedirectError struct {
	From string
	To   string
}

func (e *CrossOriginRedirectError) Error() string {
	return fmt.Sprintf("refusing cross-origin redirect from %s to %s", e.From, e.To)
}

// CanonicalShareURL validates and normalizes a Crit deployment base URL.
func CanonicalShareURL(raw string) (string, error) { //nolint:gocyclo // URL policy validation is intentionally centralized.
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("share target URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid share target URL %q", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("share target URL must use HTTP or HTTPS")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("share target URL must not contain credentials, a query, or a fragment")
	}
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if u.Scheme == "http" && !isDevelopmentShareHost(hostname) {
		return "", fmt.Errorf("share target URL must use HTTPS (HTTP is allowed only for loopback/development hosts)")
	}
	u.Host = hostname
	if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = ""
	}
	u.RawPath = ""
	return u.String(), nil
}

func isDevelopmentShareHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SameOriginRedirectPolicy permits redirects only within the original
// scheme/hostname/effective-port boundary.
func SameOriginRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || sameOrigin(via[0].URL, req.URL) {
		return nil
	}
	return &CrossOriginRedirectError{From: originString(via[0].URL), To: originString(req.URL)}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}

func originString(u *url.URL) string {
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// ResolveShareTargets applies the legacy compatibility matrix and validates
// every resulting target. The returned slice is detached from Config.
func ResolveShareTargets(cfg Config) ([]ShareTarget, error) {
	var targets []ShareTarget
	switch {
	case cfg.shareTargetsPresent || cfg.ShareTargets != nil:
		targets = append(targets, cfg.ShareTargets...)
	case cfg.shareURLPresent:
		if cfg.ShareURL == "" {
			return []ShareTarget{}, nil
		}
		targets = []ShareTarget{legacyTarget(cfg, cfg.ShareURL)}
	default:
		urlValue := cfg.ShareURL
		if urlValue == "" {
			urlValue = DefaultShareURL
		}
		targets = []ShareTarget{legacyTarget(cfg, urlValue)}
	}

	defaults := 0
	seen := make(map[string]struct{}, len(targets))
	for i := range targets {
		canonical, err := CanonicalShareURL(targets[i].URL)
		if err != nil {
			return nil, fmt.Errorf("share_targets[%d]: %w", i, err)
		}
		if _, ok := seen[canonical]; ok {
			return nil, fmt.Errorf("duplicate share target URL %q", canonical)
		}
		seen[canonical] = struct{}{}
		targets[i].URL = canonical
		if targets[i].Name == "" {
			targets[i].Name = targetDisplayName(canonical)
		}
		if canonical != DefaultShareURL {
			targets[i].ShareConsented = false
		}
		if targets[i].Default {
			defaults++
		}
	}
	if defaults > 1 {
		return nil, errors.New("share_targets may contain at most one default target")
	}
	return targets, nil
}

func legacyTarget(cfg Config, rawURL string) ShareTarget {
	return ShareTarget{
		URL: rawURL, Default: true, ProxyAuth: cfg.ProxyAuth, ShareConsented: cfg.ShareConsented,
		Auth: TargetAuth{Token: cfg.AuthToken, UserID: cfg.AuthUserID, UserName: cfg.AuthUserName, UserEmail: cfg.AuthUserEmail},
	}
}

func targetDisplayName(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" {
		if u.Path != "" && u.Path != "/" {
			return u.Host + strings.TrimRight(u.Path, "/")
		}
		return u.Host
	}
	return raw
}

// SelectShareTarget applies explicit flag/env precedence, then configured
// default rules. Explicit empty values disable sharing for the invocation.
func SelectShareTarget(flagValue string, flagPresent bool, cfg Config) (ShareTarget, bool, error) {
	targets, err := ResolveShareTargets(cfg)
	if err != nil {
		return ShareTarget{}, false, err
	}
	explicit := flagValue
	hasExplicit := flagPresent
	if !hasExplicit {
		explicit, hasExplicit = os.LookupEnv("CRIT_SHARE_URL")
	}
	if hasExplicit {
		if explicit == "" {
			return ShareTarget{}, false, nil
		}
		canonical, err := CanonicalShareURL(explicit)
		if err != nil {
			return ShareTarget{}, false, err
		}
		for _, target := range targets {
			if target.URL == canonical {
				return applyAuthEnv(target), true, nil
			}
		}
		return applyAuthEnv(ShareTarget{Name: targetDisplayName(canonical), URL: canonical}), true, nil
	}
	if len(targets) == 0 {
		return ShareTarget{}, false, nil
	}
	if len(targets) == 1 {
		return applyAuthEnv(targets[0]), true, nil
	}
	for _, target := range targets {
		if target.Default {
			return applyAuthEnv(target), true, nil
		}
	}
	return ShareTarget{}, false, errors.New("multiple share targets are configured; use --share-url to choose one or mark one as default")
}

func applyAuthEnv(target ShareTarget) ShareTarget {
	if token, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		target.Auth.Token = token
	}
	return target
}

// FindShareTarget returns the exact canonical configured target.
func FindShareTarget(cfg Config, rawURL string) (ShareTarget, bool, error) {
	canonical, err := CanonicalShareURL(rawURL)
	if err != nil {
		return ShareTarget{}, false, err
	}
	targets, err := ResolveShareTargets(cfg)
	if err != nil {
		return ShareTarget{}, false, err
	}
	for _, target := range targets {
		if target.URL == canonical {
			return applyAuthEnv(target), true, nil
		}
	}
	return ShareTarget{}, false, nil
}

func (t ShareTarget) NeedsShareConsent() bool { return t.URL == DefaultShareURL && !t.ShareConsented }

// InferShareBaseURL identifies the deployment for a legacy review URL. It
// prefers the longest configured path-prefix boundary; root deployments using
// the conventional /r/<token> route can be inferred from their origin.
func InferShareBaseURL(sharedURL string, targets []ShareTarget) (string, bool) {
	u, err := url.Parse(sharedURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	best := ""
	bestPathLen := -1
	for _, target := range targets {
		t, err := url.Parse(target.URL)
		if err != nil || !sameOrigin(t, u) {
			continue
		}
		basePath := strings.TrimRight(t.Path, "/")
		if basePath == "" || u.Path == basePath || strings.HasPrefix(u.Path, basePath+"/") {
			if len(basePath) > bestPathLen {
				best = target.URL
				bestPathLen = len(basePath)
			}
		}
	}
	if best != "" {
		return best, true
	}
	if strings.HasPrefix(u.Path, "/r/") {
		base, err := CanonicalShareURL(u.Scheme + "://" + u.Host)
		return base, err == nil
	}
	return "", false
}

// MutateShareTargets materializes legacy singleton sharing state and applies a
// target mutation in the same locked atomic config transaction.
func MutateShareTargets(mutate func(*[]ShareTarget) error) error {
	return SaveGlobalConfig(func(raw map[string]json.RawMessage) error {
		var cfg Config
		data, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return err
		}
		_, cfg.shareTargetsPresent = raw["share_targets"]
		_, cfg.shareURLPresent = raw["share_url"]
		targets, err := ResolveShareTargets(cfg)
		if err != nil {
			return err
		}
		if err := mutate(&targets); err != nil {
			return err
		}
		if _, err := ResolveShareTargets(Config{ShareTargets: targets, shareTargetsPresent: true}); err != nil {
			return err
		}
		encoded, err := json.Marshal(targets)
		if err != nil {
			return err
		}
		raw["share_targets"] = encoded
		for _, key := range []string{"share_url", "proxy_auth", "auth_token", "auth_user_id", "auth_user_name", "auth_user_email", "share_consented"} {
			delete(raw, key)
		}
		return nil
	})
}

// SaveTargetConsent marks consent only on the canonical public target.
func SaveTargetConsent(targetURL string) error {
	canonical, err := CanonicalShareURL(targetURL)
	if err != nil {
		return err
	}
	if canonical != DefaultShareURL {
		return nil
	}
	return SaveGlobalConfig(func(raw map[string]json.RawMessage) error {
		encoded, hasTargets := raw["share_targets"]
		if !hasTargets {
			raw["share_consented"] = json.RawMessage("true")
			return nil
		}
		var targets []ShareTarget
		if err := json.Unmarshal(encoded, &targets); err != nil {
			return err
		}
		for i := range targets {
			targetCanonical, err := CanonicalShareURL(targets[i].URL)
			if err != nil {
				return err
			}
			if targetCanonical == canonical {
				targets[i].ShareConsented = true
				updated, err := json.Marshal(targets)
				if err != nil {
					return err
				}
				raw["share_targets"] = updated
				return nil
			}
		}
		// Ephemeral public target: consent is intentionally process-local.
		return nil
	})
}
