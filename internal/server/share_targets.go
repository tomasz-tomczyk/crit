package server

import (
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

type shareTargetMetadata struct {
	Name              string `json:"name"`
	URL               string `json:"url"`
	Default           bool   `json:"default,omitempty"`
	ProxyAuth         bool   `json:"proxy_auth,omitempty"`
	AuthLoggedIn      bool   `json:"auth_logged_in"`
	AuthUserName      string `json:"auth_user_name,omitempty"`
	AuthUserEmail     string `json:"auth_user_email,omitempty"`
	NeedsShareConsent bool   `json:"needs_share_consent"`
}

// freshShareConfig deliberately reloads low-frequency sharing configuration.
// Each handler calls it once and holds the returned immutable value throughout
// the request, so an external auth/config write is visible without daemon
// restart but cannot mix credentials from two snapshots.
func (s *Server) freshShareConfig() config.Config {
	if s.configConfigured && s.projectDir != "" {
		cfg := config.LoadConfigForCommands(s.projectDir)
		if s.cfg.RuntimeShareURL != nil {
			override := *s.cfg.RuntimeShareURL
			cfg.RuntimeShareURL = &override
			if override == "" {
				cfg.ShareTargets = []config.ShareTarget{}
			} else if target, ok, err := config.SelectShareTarget(override, true, cfg); err == nil && ok {
				cfg.ShareTargets = []config.ShareTarget{target}
			}
		}
		return cfg
	}
	return s.cfg
}

func (s *Server) resolvedShareTargets() ([]config.ShareTarget, error) {
	if !s.configConfigured {
		if s.shareURL == "" {
			return []config.ShareTarget{}, nil
		}
		canonical, err := config.CanonicalShareURL(s.shareURL)
		if err != nil {
			return nil, err
		}
		return []config.ShareTarget{{Name: canonical, URL: canonical, Default: true, ProxyAuth: s.proxyAuth, ShareConsented: s.cfg.ShareConsented, Auth: config.TargetAuth{Token: s.authTokenSnapshot(), UserID: s.cfg.AuthUserID, UserName: s.cfg.AuthUserName, UserEmail: s.cfg.AuthUserEmail}}}, nil
	}
	cfg := s.freshShareConfig()
	targets, err := config.ResolveShareTargets(cfg)
	if err != nil {
		return nil, err
	}
	// NewServer's legacy constructor is widely used by embedders/tests. Treat
	// its explicit URL as one ephemeral target when daemon config is absent.
	if !s.configConfigured && s.shareURL != "" {
		canonical, canonicalErr := config.CanonicalShareURL(s.shareURL)
		if canonicalErr != nil {
			return nil, canonicalErr
		}
		found := false
		for i := range targets {
			if targets[i].URL == canonical {
				targets[i].ProxyAuth = s.proxyAuth
				if targets[i].Auth.Token == "" {
					targets[i].Auth.Token = s.authTokenSnapshot()
				}
				found = true
			}
		}
		if !found {
			targets = []config.ShareTarget{{Name: canonical, URL: canonical, Default: true, ProxyAuth: s.proxyAuth, Auth: config.TargetAuth{Token: s.authTokenSnapshot(), UserID: s.cfg.AuthUserID, UserName: s.cfg.AuthUserName, UserEmail: s.cfg.AuthUserEmail}, ShareConsented: s.cfg.ShareConsented}}
		}
	}
	return targets, nil
}

func (s *Server) targetForRequest(requested string) (config.ShareTarget, error) { //nolint:gocyclo // Bound, requested, and default target rules are intentionally explicit.
	targets, err := s.resolvedShareTargets()
	if err != nil {
		return config.ShareTarget{}, err
	}
	sess := s.session.Load()
	bound := ""
	if sess != nil && sess.GetSharedURL() != "" {
		bound = sess.GetShareBaseURL()
		if bound == "" {
			if inferred, ok := config.InferShareBaseURL(sess.GetSharedURL(), targets); ok {
				bound = inferred
			}
		}
	}
	if requested != "" {
		canonical, err := config.CanonicalShareURL(requested)
		if err != nil {
			return config.ShareTarget{}, err
		}
		if bound != "" && canonical != bound {
			return config.ShareTarget{}, fmt.Errorf("review is bound to %s", bound)
		}
		for _, target := range targets {
			if target.URL == canonical {
				return applyServerAuthEnv(target), nil
			}
		}
		return config.ShareTarget{}, fmt.Errorf("unknown share target %s", canonical)
	}
	if len(targets) == 0 {
		return config.ShareTarget{}, fmt.Errorf("sharing is disabled")
	}
	if bound != "" {
		for _, target := range targets {
			if target.URL == bound {
				return applyServerAuthEnv(target), nil
			}
		}
		return config.ShareTarget{}, fmt.Errorf("originating Crit instance %s is no longer configured", bound)
	}
	cfg := s.freshShareConfig()
	if !s.configConfigured && s.shareURL != "" {
		for _, target := range targets {
			if target.URL == s.shareURL {
				return applyServerAuthEnv(target), nil
			}
		}
	}
	target, ok, err := config.SelectShareTarget("", false, cfg)
	if err != nil {
		return config.ShareTarget{}, err
	}
	if !ok {
		return config.ShareTarget{}, fmt.Errorf("sharing is disabled")
	}
	return target, nil
}

func applyServerAuthEnv(target config.ShareTarget) config.ShareTarget {
	if token, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		target.Auth.Token = token
	}
	return target
}

func targetMetadata(targets []config.ShareTarget) []shareTargetMetadata {
	out := make([]shareTargetMetadata, len(targets))
	for i, target := range targets {
		out[i] = shareTargetMetadata{Name: target.Name, URL: target.URL, Default: target.Default, ProxyAuth: target.ProxyAuth, AuthLoggedIn: target.Auth.Token != "", AuthUserName: target.Auth.UserName, AuthUserEmail: target.Auth.UserEmail, NeedsShareConsent: target.NeedsShareConsent()}
	}
	return out
}
