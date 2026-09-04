package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/story"
)

// handleStory serves GET/POST/DELETE /api/story (spec §6, §7.1, §9 Phase 5).
// POST/DELETE run the same story.Ingest validation the `crit story` CLI uses,
// persist via SyncWriteFiles, and broadcast an SSE story-updated event so the
// renderer live-updates without a manual refetch.
func (s *Server) handleStory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleStoryGet(w, r)
	case http.MethodPost:
		s.handleStoryPost(w, r)
	case http.MethodDelete:
		s.handleStoryDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStoryGet(w http.ResponseWriter, _ *http.Request) {
	st := s.session.Load().GetStory()
	if st == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, st)
}

// handleStoryPost validates the posted story against the live diff scope via
// story.Ingest, saves it on success (overwriting idempotently), and rejects
// with the coverage report JSON body on failure. Nothing is saved or
// broadcast on rejection.
func (s *Server) handleStoryPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	var req struct {
		Story *session.Story `json:"story"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Story == nil {
		http.Error(w, "Bad request: story is required", http.StatusBadRequest)
		return
	}

	sess := s.session.Load()
	scope := sess.StoryScope(sess.IgnorePatterns)
	_, indexed, ignored := story.FromScope(scope)

	res, ingestErr := story.Run(story.Ingest{
		Story:           req.Story,
		Indexed:         indexed,
		Ignored:         ignored,
		LiveFingerprint: story.Fingerprint(indexed),
	})
	if ingestErr != nil {
		// Body shape matches exactly what `crit story`'s printCoverage prints
		// to stdout on rejection: the bare StoryCoverage JSON object. The
		// rejection reason goes in a header rather than the body so callers
		// parsing the coverage report don't need to special-case an
		// error-wrapper shape.
		w.Header().Set("X-Story-Ingest-Error", ingestErr.Error())
		writeJSONStatus(w, http.StatusUnprocessableEntity, res.Coverage)
		return
	}

	sess.SetStory(req.Story)
	if err := sess.SyncWriteFiles(); err != nil {
		http.Error(w, fmt.Sprintf("saving story: %v", err), http.StatusInternalServerError)
		return
	}
	notifyStoryUpdated(sess, req.Story)
	writeJSON(w, req.Story)
}

func (s *Server) handleStoryDelete(w http.ResponseWriter, _ *http.Request) {
	sess := s.session.Load()
	sess.ClearStory()
	if err := sess.SyncWriteFiles(); err != nil {
		http.Error(w, fmt.Sprintf("saving story: %v", err), http.StatusInternalServerError)
		return
	}
	notifyStoryUpdated(sess, nil)
	w.WriteHeader(http.StatusNoContent)
}

// notifyStoryUpdated broadcasts the story-updated SSE event. The payload
// echoes the new story (or null after DELETE) so the renderer doesn't have to
// refetch — mirrors the focus-changed event's JSON-in-Content convention.
func notifyStoryUpdated(sess *Session, st *session.Story) {
	payload, _ := json.Marshal(map[string]any{"story": st})
	sess.Notify(SSEEvent{Type: "story-updated", Content: string(payload)})
}
