package comment

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

const previewContentRoute = "/preview-content"

func (e BulkCommentEntry) pinSelector() string {
	if e.Selector != "" {
		return e.Selector
	}
	return e.CSSSelector
}

func (e BulkCommentEntry) pinRoute() string {
	if e.Route != "" {
		return e.Route
	}
	return e.Pathname
}

// addPinComment appends a DOM pin to a live or preview review and writes it.
func addPinComment(critPath, selector, route, body, author, userID string) (session.Comment, error) {
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		return session.Comment{}, err
	}
	c, err := appendPin(&cj, selector, route, body, author, userID, nil)
	if err != nil {
		return session.Comment{}, err
	}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return session.Comment{}, err
	}
	return c, nil
}

// appendPin adds a live-mode pin to cj. The file key is the page route, matching
// the browser composer (POST /api/file/comments?path=<pathname>).
// pages caches live page fetches across calls; nil fetches every time.
func appendPin(cj *session.CritJSON, selector, route, body, author, userID string, pages livePageCache) (session.Comment, error) {
	if cj.ReviewType != "live" && cj.ReviewType != "preview" {
		return session.Comment{}, fmt.Errorf("--selector is only supported for live and preview reviews")
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return session.Comment{}, fmt.Errorf("selector is required")
	}
	if strings.TrimSpace(body) == "" {
		return session.Comment{}, fmt.Errorf("comment body is required")
	}
	route = normalizePinRoute(cj.ReviewType, route, cj.Origin)

	anchor := &session.DOMAnchor{
		Pathname:    route,
		CSSSelector: selector,
	}
	if err := enrichPinAnchor(cj, anchor, pages); err != nil {
		return session.Comment{}, err
	}

	if cj.Files == nil {
		cj.Files = map[string]session.CritJSONFile{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now
	cf := cj.Files[route]
	if cf.Status == "" {
		cf.Status = "added"
	}
	if cf.Comments == nil {
		cf.Comments = []session.Comment{}
	}
	round := cj.ReviewRound
	if round == 0 {
		round = 1
	}
	c := session.Comment{
		ID:          session.RandomCommentID(),
		Body:        body,
		Author:      author,
		UserID:      userID,
		DOMAnchor:   anchor,
		PinNumber:   nextPinNumber(cj),
		CreatedAt:   now,
		UpdatedAt:   now,
		ReviewRound: round,
	}
	cf.Comments = append(cf.Comments, c)
	cj.Files[route] = cf
	return c, nil
}

func nextPinNumber(cj *session.CritJSON) int {
	next := 1
	for _, file := range cj.Files {
		for _, existing := range file.Comments {
			if existing.PinNumber >= next {
				next = existing.PinNumber + 1
			}
		}
	}
	return next
}

func normalizePinRoute(kind, route, origin string) string {
	route = cleanRoute(route)
	if kind == "preview" {
		switch {
		case route == "" || route == "/":
			return previewContentRoute
		case route == previewContentRoute || strings.HasPrefix(route, previewContentRoute+"/"):
			return route
		default:
			return previewContentRoute + route
		}
	}
	if route == "" {
		return defaultLiveRoute(origin)
	}
	return route
}

func defaultLiveRoute(origin string) string {
	if origin == "" {
		return "/"
	}
	// Origin is a URL (http://localhost:3000/dashboard). Use its path when
	// the caller did not pass --route.
	if i := strings.Index(origin, "://"); i >= 0 {
		rest := origin[i+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			path := rest[slash:]
			if hash := strings.Index(path, "#"); hash >= 0 {
				path = path[:hash]
			}
			if q := strings.Index(path, "?"); q >= 0 {
				path = path[:q]
			}
			if cleaned := cleanRoute(path); cleaned != "" && cleaned != "/" {
				return cleaned
			}
		}
	}
	return "/"
}

func cleanRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return ""
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	if len(route) > 1 && strings.HasSuffix(route, "/") {
		route = strings.TrimSuffix(route, "/")
	}
	return route
}
