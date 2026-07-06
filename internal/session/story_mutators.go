package session

// GetStory returns the session's current story (nil if none).
func (s *Session) GetStory() *Story {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.story
}

// SetStory sets the session's story, overwriting whatever was there before.
// Callers persist via SyncWriteFiles and broadcast SSE separately — this only
// updates in-memory state.
func (s *Session) SetStory(st *Story) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.story = st
}

// ClearStory removes the session's story (no-op if already nil).
func (s *Session) ClearStory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.story = nil
}
