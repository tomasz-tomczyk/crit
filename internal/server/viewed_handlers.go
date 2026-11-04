package server

import (
	"encoding/json"
	"net/http"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// handleViewed serves the reviewer's viewed marks.
// GET returns {"files": {path: mark}}; PUT saves {"files": [update, ...]}.
func (s *Server) handleViewed(w http.ResponseWriter, r *http.Request) {
	sess := s.session.Load()
	switch r.Method {
	case http.MethodGet:
		files, err := sess.ViewedFiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if files == nil {
			files = map[string]session.ViewedFile{}
		}
		writeJSON(w, map[string]any{"files": files})
	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		var req struct {
			Files []session.ViewedUpdate `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		for _, f := range req.Files {
			if f.Path == "" {
				http.Error(w, "Bad request: path is required", http.StatusBadRequest)
				return
			}
		}
		if err := sess.SetViewedFiles(req.Files); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
