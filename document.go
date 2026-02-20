package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Comment struct {
	ID              string `json:"id"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	Body            string `json:"body"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Resolved        bool   `json:"resolved,omitempty"`
	ResolutionNote  string `json:"resolution_note,omitempty"`
	ResolutionLines []int  `json:"resolution_lines,omitempty"`
}

type CommentsFile struct {
	File        string    `json:"file"`
	FileHash    string    `json:"file_hash"`
	UpdatedAt   string    `json:"updated_at"`
	ShareURL    string    `json:"share_url,omitempty"`
	DeleteToken string    `json:"delete_token,omitempty"`
	Comments    []Comment `json:"comments"`
}

type SSEEvent struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type Document struct {
	FilePath         string
	FileName         string
	FileDir          string
	Content          string
	FileHash         string
	OutputDir        string
	Comments         []Comment
	PreviousContent  string    // content from the previous round (empty on first round)
	PreviousComments []Comment // comments from the previous round
	mu               sync.RWMutex
	nextID           int
	writeTimer       *time.Timer
	staleNotice      string
	sharedURL        string
	deleteToken      string
	subscribers      map[chan SSEEvent]struct{}
	subMu            sync.Mutex
	pendingEdits     int           // number of file changes detected since last round-complete
	roundComplete    chan struct{} // signaled when agent calls round-complete
	reviewRound      int           // current review round (1-based)
}

func NewDocument(filePath, outputDir string) (*Document, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))

	doc := &Document{
		FilePath:      filePath,
		FileName:      filepath.Base(filePath),
		FileDir:       filepath.Dir(filePath),
		Content:       content,
		FileHash:      hash,
		OutputDir:     outputDir,
		Comments:      []Comment{},
		nextID:        1,
		reviewRound:   1,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}

	doc.loadComments()
	return doc, nil
}

func (d *Document) commentsFilePath() string {
	return filepath.Join(d.OutputDir, "."+d.FileName+".comments.json")
}

func (d *Document) reviewFilePath() string {
	ext := filepath.Ext(d.FileName)
	base := strings.TrimSuffix(d.FileName, ext)
	return filepath.Join(d.OutputDir, base+".review"+ext)
}

func (d *Document) loadComments() {
	data, err := os.ReadFile(d.commentsFilePath())
	if err != nil {
		return
	}

	var cf CommentsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return
	}

	// Load share URL regardless of file hash so it persists even after file changes.
	d.sharedURL = cf.ShareURL
	d.deleteToken = cf.DeleteToken

	if cf.FileHash != d.FileHash {
		d.staleNotice = "The source file has changed since the last review session. Previous comments may not align with the current content."
		return
	}

	d.Comments = cf.Comments
	for _, c := range d.Comments {
		id := 0
		_, _ = fmt.Sscanf(c.ID, "c%d", &id)
		if id >= d.nextID {
			d.nextID = id + 1
		}
	}
}

func (d *Document) AddComment(startLine, endLine int, body string) Comment {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	c := Comment{
		ID:        fmt.Sprintf("c%d", d.nextID),
		StartLine: startLine,
		EndLine:   endLine,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}
	d.nextID++
	d.Comments = append(d.Comments, c)
	d.scheduleWrite()
	return c
}

func (d *Document) UpdateComment(id, body string) (Comment, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, c := range d.Comments {
		if c.ID == id {
			d.Comments[i].Body = body
			d.Comments[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			d.scheduleWrite()
			return d.Comments[i], true
		}
	}
	return Comment{}, false
}

func (d *Document) DeleteComment(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, c := range d.Comments {
		if c.ID == id {
			d.Comments = append(d.Comments[:i], d.Comments[i+1:]...)
			d.scheduleWrite()
			return true
		}
	}
	return false
}

func (d *Document) GetComments() []Comment {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Comment, len(d.Comments))
	copy(result, d.Comments)
	return result
}

func (d *Document) GetStaleNotice() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.staleNotice
}

func (d *Document) ClearStaleNotice() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.staleNotice = ""
}

func (d *Document) GetSharedURL() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sharedURL
}

func (d *Document) SetSharedURL(url string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sharedURL = url
	d.scheduleWrite()
}

func (d *Document) GetDeleteToken() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.deleteToken
}

func (d *Document) SetDeleteToken(token string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteToken = token
	d.scheduleWrite()
}

// SetSharedURLAndToken atomically updates both the shared URL and delete token.
func (d *Document) SetSharedURLAndToken(url, token string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sharedURL = url
	d.deleteToken = token
	d.scheduleWrite()
}

func (d *Document) IncrementEdits() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingEdits++
}

func (d *Document) GetPendingEdits() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.pendingEdits
}

func (d *Document) SignalRoundComplete() {
	d.mu.Lock()
	d.pendingEdits = 0
	d.reviewRound++
	d.Comments = []Comment{}
	d.nextID = 1
	d.mu.Unlock()
	select {
	case d.roundComplete <- struct{}{}:
	default:
	}
}

func (d *Document) RoundCompleteChan() <-chan struct{} {
	return d.roundComplete
}

func (d *Document) scheduleWrite() {
	if d.writeTimer != nil {
		d.writeTimer.Stop()
	}
	d.writeTimer = time.AfterFunc(200*time.Millisecond, func() {
		d.WriteFiles()
	})
}

func (d *Document) WriteFiles() {
	d.mu.RLock()
	comments := make([]Comment, len(d.Comments))
	copy(comments, d.Comments)
	sharedURL := d.sharedURL
	deleteToken := d.deleteToken
	d.mu.RUnlock()

	d.writeCommentsJSON(comments, sharedURL, deleteToken)
	d.writeReviewMD(comments)
}

func (d *Document) writeCommentsJSON(comments []Comment, sharedURL, deleteToken string) {
	if len(comments) == 0 && sharedURL == "" && deleteToken == "" {
		os.Remove(d.commentsFilePath())
		return
	}

	cf := CommentsFile{
		File:        d.FileName,
		FileHash:    d.FileHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		ShareURL:    sharedURL,
		DeleteToken: deleteToken,
		Comments:    comments,
	}

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling comments: %v\n", err)
		return
	}

	if err := os.WriteFile(d.commentsFilePath(), data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing comments file: %v\n", err)
	}
}

func (d *Document) writeReviewMD(comments []Comment) {
	if len(comments) == 0 {
		os.Remove(d.reviewFilePath())
		return
	}

	reviewContent := GenerateReviewMD(d.Content, comments)

	if err := os.WriteFile(d.reviewFilePath(), []byte(reviewContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing review file: %v\n", err)
	}
}

// SSE subscriber management

func (d *Document) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 4)
	d.subMu.Lock()
	d.subscribers[ch] = struct{}{}
	d.subMu.Unlock()
	return ch
}

func (d *Document) Unsubscribe(ch chan SSEEvent) {
	d.subMu.Lock()
	delete(d.subscribers, ch)
	d.subMu.Unlock()
	close(ch)
}

func (d *Document) notify(event SSEEvent) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	for ch := range d.subscribers {
		select {
		case ch <- event:
		default:
			// drop if subscriber is slow
		}
	}
}

// Shutdown sends a server-shutdown event to all SSE subscribers so the
// browser page can show an appropriate message.
func (d *Document) Shutdown() {
	d.notify(SSEEvent{Type: "server-shutdown"})
}

// ReloadFile re-reads the source file and clears in-memory comments.
// The .review.md and .comments.json files are kept so the agent can
// reference and modify them while editing.
func (d *Document) ReloadFile() error {
	data, err := os.ReadFile(d.FilePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	d.mu.Lock()
	// Only snapshot on the first edit of a round (pendingEdits == 0).
	// Subsequent edits within the same round keep the original snapshot
	// so the diff covers all changes since the round started.
	if d.pendingEdits == 0 {
		d.PreviousContent = d.Content
		d.PreviousComments = make([]Comment, len(d.Comments))
		copy(d.PreviousComments, d.Comments)
	}

	d.Content = string(data)
	d.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	d.Comments = []Comment{}
	d.nextID = 1
	d.staleNotice = ""
	d.mu.Unlock()

	return nil
}

// loadResolvedComments reads the .comments.json file to pick up any
// resolved fields the agent wrote during the editing round.
func (d *Document) loadResolvedComments() {
	data, err := os.ReadFile(d.commentsFilePath())
	if err != nil {
		return
	}
	var cf CommentsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return
	}
	d.mu.Lock()
	d.PreviousComments = cf.Comments
	d.mu.Unlock()
}

// WatchFile polls the source file for changes every second.
// On change, it reloads the file, increments the edit counter, and sends an
// "edit-detected" SSE event. The full "file-changed" event is deferred until
// the agent signals round completion via the roundComplete channel.
func (d *Document) WatchFile(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			data, err := os.ReadFile(d.FilePath)
			if err != nil {
				continue
			}
			hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))

			d.mu.RLock()
			changed := hash != d.FileHash
			d.mu.RUnlock()

			if changed {
				if err := d.ReloadFile(); err != nil {
					fmt.Fprintf(os.Stderr, "Error reloading file: %v\n", err)
					continue
				}
				d.IncrementEdits()

				// Notify frontend of edit detection (for counter in waiting modal)
				d.notify(SSEEvent{
					Type:     "edit-detected",
					Filename: d.FileName,
					Content:  fmt.Sprintf("%d", d.GetPendingEdits()),
				})
			}
		case <-d.roundComplete:
			// Load agent's resolved comments from .comments.json before cleanup
			d.loadResolvedComments()
			os.Remove(d.commentsFilePath())
			os.Remove(d.reviewFilePath())

			// Agent signaled round complete — send the full file-changed event
			d.mu.RLock()
			event := SSEEvent{
				Type:     "file-changed",
				Filename: d.FileName,
				Content:  d.Content,
			}
			d.mu.RUnlock()

			d.notify(event)
		}
	}
}
