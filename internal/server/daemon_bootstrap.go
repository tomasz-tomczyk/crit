package server

import (
	"context"
	"time"
)

// ConfigureDaemon sets fields assigned by cmd/crit during daemon startup.
func (s *Server) ConfigureDaemon(cfg Config, projectDir, homeDir, reviewPath string, cliArgs []string, startedAt time.Time) {
	s.cfg = cfg
	s.projectDir = projectDir
	s.homeDir = homeDir
	s.reviewPath = reviewPath
	s.cliArgs = cliArgs
	s.sessionStartedAt = startedAt
}

// SetIntegrationWarnings records stale/missing agent integration hints for the UI.
func (s *Server) SetIntegrationWarnings(stale []StaleIntegration, missing []string) {
	s.staleIntegrations = stale
	s.missingIntegrations = missing
}

// PrimePRListCache warms the open-PR picker cache in the background.
func (s *Server) PrimePRListCache(ctx context.Context) {
	if s.prList != nil {
		go func() { _, _ = s.prList.GetCtx(ctx) }()
	}
}

// ShouldRecordStatsOnShutdown reports whether shutdown should write session stats.
func (s *Server) ShouldRecordStatsOnShutdown() bool {
	return !s.statsRecorded && !s.cfg.DisableStats
}

// Author returns the daemon's configured comment author name.
func (s *Server) Author() string {
	return s.author
}
