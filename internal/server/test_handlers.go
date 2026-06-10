package server

import "net/http"

// HandlePreviewPayloadForTest exposes handlePreviewPayload for cross-package tests.
func (s *Server) HandlePreviewPayloadForTest(w http.ResponseWriter, r *http.Request) {
	s.handlePreviewPayload(w, r)
}

// HandleUpsertPayloadForTest exposes handleUpsertPayload for cross-package tests.
func (s *Server) HandleUpsertPayloadForTest(w http.ResponseWriter, r *http.Request) {
	s.handleUpsertPayload(w, r)
}

// HandleFileCommentsForTest exposes handleFileComments for cross-package tests.
func (s *Server) HandleFileCommentsForTest(w http.ResponseWriter, r *http.Request) {
	s.handleFileComments(w, r)
}

// HandlePreviewContentForTest exposes handlePreviewContent for cross-package tests.
func (s *Server) HandlePreviewContentForTest(w http.ResponseWriter, r *http.Request) {
	s.handlePreviewContent(w, r)
}

// ServeIndexHTMLForTest returns the embedded index.html shell handler.
func (s *Server) ServeIndexHTMLForTest() http.HandlerFunc {
	return s.serveIndexHTML()
}

// SetReviewPathForTest sets the review identity path used by attachment handlers.
func (s *Server) SetReviewPathForTest(path string) {
	s.reviewPath = path
}

// HandleAttachmentsForTest exposes handleAttachments for cross-package tests.
func (s *Server) HandleAttachmentsForTest(w http.ResponseWriter, r *http.Request) {
	s.handleAttachments(w, r)
}

// SeedPRListForTest pre-populates the picker PR cache so tests skip gh.
func (s *Server) SeedPRListForTest() {
	if s.prList == nil {
		s.prList = &PRListCache{}
	}
	s.prList.SeedForTest(nil)
}

// SetAuthorForTest sets the default comment author for tests.
func (s *Server) SetAuthorForTest(author string) {
	s.author = author
}

// LoadSessionForTest returns the current session snapshot for tests.
func (s *Server) LoadSessionForTest() *Session {
	return s.session.Load()
}

// StoreSessionForTest replaces the current session for tests.
func (s *Server) StoreSessionForTest(sess *Session) {
	s.session.Store(sess)
}
