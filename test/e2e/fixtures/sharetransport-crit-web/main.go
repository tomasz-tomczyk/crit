// Package main is the share-transport E2E fixture: a minimal stand-in for a
// crit-web instance, implementing only the four endpoints the local crit
// server calls when sharing (POST/PUT/DELETE /api/reviews and
// GET /api/reviews/:token/comments).
//
// Its purpose is to let the share-transport Playwright project drive the real
// browser Share flow (share -> pull -> re-share -> unpublish) end to end
// without a Postgres-backed crit-web. Every /api request is recorded together
// with its Origin header, which is what makes the key regression assertion
// possible: browser-issued cross-origin fetches carry an Origin, requests
// proxied by the local Go server do not. See tests/share-transport.sharetransport.spec.ts.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// recordedRequest is one /api call the fixture received. Origin is the raw
// Origin header — empty for server-to-server calls, set for browser fetches.
type recordedRequest struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Origin  string `json:"origin"`
	HasAuth bool   `json:"has_auth"`
}

type review struct {
	DeleteToken string            `json:"delete_token"`
	ReviewRound int               `json:"review_round"`
	Comments    []json.RawMessage `json:"comments"`
}

type fixture struct {
	origin string

	mu         sync.Mutex
	reviews    map[string]*review
	log        []recordedRequest
	nextToken  int
	failDelete bool
}

func main() {
	port := flag.Int("port", 0, "TCP port to listen on (0 = pick a free port)")
	flag.Parse()

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	f := &fixture{
		origin:  "http://" + ln.Addr().String(),
		reviews: map[string]*review{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/reviews", f.handleReviewsCollection)
	mux.HandleFunc("/api/reviews/", f.handleReviewByToken)
	mux.HandleFunc("/api/share-policy", f.handleSharePolicy)
	mux.HandleFunc("/__seed-comment", f.handleSeedComment)
	mux.HandleFunc("/__config", f.handleConfig)
	mux.HandleFunc("/__log", f.handleLog)
	mux.HandleFunc("/__reset", f.handleReset)

	// Stable stdout contract used by setup-fixtures-sharetransport.sh.
	fmt.Printf("listening on %s\n", f.origin)
	_ = os.Stdout.Sync()

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(ln); err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Fatal(err)
	}
}

func (f *fixture) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, recordedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Origin:  r.Header.Get("Origin"),
		HasAuth: r.Header.Get("Authorization") != "",
	})
}

// POST /api/reviews creates a review; DELETE /api/reviews unpublishes one.
func (f *fixture) handleReviewsCollection(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	switch r.Method {
	case http.MethodPost:
		f.createReview(w)
	case http.MethodDelete:
		f.deleteReview(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fixture) createReview(w http.ResponseWriter) {
	f.mu.Lock()
	f.nextToken++
	token := fmt.Sprintf("tok%d", f.nextToken)
	f.reviews[token] = &review{DeleteToken: "del-" + token, ReviewRound: 1}
	f.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"url":          f.origin + "/r/" + token,
		"delete_token": "del-" + token,
	})
}

func (f *fixture) deleteReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeleteToken string `json:"delete_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unpublish rejected by fixture"})
		return
	}
	for token, rev := range f.reviews {
		if rev.DeleteToken == body.DeleteToken {
			delete(f.reviews, token)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	// Unknown token — crit treats 404 as "already gone", i.e. success.
	w.WriteHeader(http.StatusNotFound)
}

// GET /api/reviews/:token/comments lists seeded comments; PUT /api/reviews/:token
// upserts the review and bumps its round.
func (f *fixture) handleReviewByToken(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	rest := strings.TrimPrefix(r.URL.Path, "/api/reviews/")
	token := strings.TrimSuffix(rest, "/comments")
	isComments := strings.HasSuffix(rest, "/comments")

	f.mu.Lock()
	rev, ok := f.reviews[token]
	f.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	switch {
	case isComments && r.Method == http.MethodGet:
		f.mu.Lock()
		comments := append([]json.RawMessage{}, rev.Comments...)
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, comments)
	case !isComments && r.Method == http.MethodPut:
		f.mu.Lock()
		rev.ReviewRound++
		round := rev.ReviewRound
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"url":          f.origin + "/r/" + token,
			"review_round": round,
			"changed":      true,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/share-policy — the local server proxies this to populate the org
// picker. An empty policy keeps the fixture in the anonymous/no-org path.
func (f *fixture) handleSharePolicy(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	writeJSON(w, http.StatusOK, map[string]any{"orgs": []any{}})
}

// POST /__seed-comment?token=<token> appends a comment to the review, standing
// in for someone commenting in the browser on the hosted page.
func (f *fixture) handleSeedComment(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	raw, err := readAllJSON(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	rev, ok := f.reviews[token]
	if !ok {
		http.Error(w, "unknown token "+token, http.StatusNotFound)
		return
	}
	rev.Comments = append(rev.Comments, raw)
	w.WriteHeader(http.StatusNoContent)
}

// POST /__config toggles fault injection, e.g. {"fail_delete": true}.
func (f *fixture) handleConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FailDelete bool `json:"fail_delete"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.failDelete = body.FailDelete
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fixture) handleLog(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	entries := append([]recordedRequest{}, f.log...)
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, entries)
}

func (f *fixture) handleReset(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	f.reviews = map[string]*review{}
	f.log = nil
	f.failDelete = false
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func readAllJSON(r *http.Request) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	return raw, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
