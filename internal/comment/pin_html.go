package comment

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

const pinOuterHTMLMax = 2048

// readLivePageHTML fetches a live-review page so a pin can record the same
// tag chain the browser agent would. Tests replace it.
var readLivePageHTML = readLivePageHTMLDefault

// livePageCache holds the live pages fetched in one CLI run, keyed by route,
// so a bulk run with many pins on one page fetches it once. A nil cache
// fetches every time.
type livePageCache map[string]livePageResult

type livePageResult struct {
	data []byte
	err  error
}

func (c livePageCache) read(origin, route string) ([]byte, error) {
	if c == nil {
		return readLivePageHTML(origin, route)
	}
	if r, ok := c[route]; ok {
		return r.data, r.err
	}
	data, err := readLivePageHTML(origin, route)
	c[route] = livePageResult{data: data, err: err}
	return data, err
}

func enrichPinAnchor(cj *session.CritJSON, anchor *session.DOMAnchor, pages livePageCache) error {
	sel, err := cascadia.Parse(anchor.CSSSelector)
	if err != nil {
		return fmt.Errorf("invalid selector %q", anchor.CSSSelector)
	}
	switch cj.ReviewType {
	case "preview":
		return enrichPreviewAnchor(cj, anchor, sel)
	case "live":
		enrichLiveAnchor(cj, anchor, sel, pages)
		return nil
	default:
		return fmt.Errorf("--selector is only supported for live and preview reviews")
	}
}

func enrichPreviewAnchor(cj *session.CritJSON, anchor *session.DOMAnchor, sel cascadia.Sel) error {
	filePath, err := previewHTMLFile(cj, anchor.Pathname)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading preview file: %w", err)
	}
	if err := fillAnchorFromHTML(anchor, sel, data); err != nil {
		return fmt.Errorf("selector %q not found in %s", anchor.CSSSelector, filePath)
	}
	return nil
}

// enrichLiveAnchor fills the anchor from the running app when the element is
// in the HTML response. Pages that render the element in JavaScript still get
// a pin: a tag in the selector (button#save) is enough for the browser to
// attach it. A selector that matches nothing static and has no tag is stored
// as-is and still shows in the comment list.
func enrichLiveAnchor(cj *session.CritJSON, anchor *session.DOMAnchor, sel cascadia.Sel, pages livePageCache) {
	if data, err := pages.read(cj.Origin, anchor.Pathname); err == nil && len(data) > 0 {
		if err := fillAnchorFromHTML(anchor, sel, data); err == nil {
			return
		}
	}
	if tag := leafTagFromSelector(anchor.CSSSelector); tag != "" {
		anchor.TagChain = []string{tag}
	}
}

func fillAnchorFromHTML(anchor *session.DOMAnchor, sel cascadia.Sel, page []byte) error {
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return err
	}
	el := cascadia.Query(doc, sel)
	if el == nil {
		return fmt.Errorf("not found")
	}
	anchor.TagChain = tagChain(el)
	anchor.OuterHTML = truncateRunes(renderNode(el), pinOuterHTMLMax)
	anchor.AccessibleName = accessibleName(el)
	anchor.Role = roleFor(el)
	anchor.Landmark = landmarkFor(el)
	return nil
}

func previewHTMLFile(cj *session.CritJSON, route string) (string, error) {
	origin := cj.Origin
	if origin == "" && len(cj.CliArgs) >= 2 && cj.CliArgs[0] == "preview" {
		origin = cj.CliArgs[1]
	}
	if origin == "" {
		return "", fmt.Errorf("preview review has no HTML file")
	}
	rel := strings.TrimPrefix(route, previewContentRoute)
	if rel == "" || rel == "/" {
		return origin, nil
	}
	rel = strings.TrimPrefix(rel, "/")
	base := filepath.Dir(origin)
	resolved := filepath.Join(base, filepath.Clean(rel))
	prefix := base + string(filepath.Separator)
	if resolved != base && !strings.HasPrefix(resolved, prefix) {
		return "", fmt.Errorf("route %q escapes the preview directory", route)
	}
	return resolved, nil
}

func readLivePageHTMLDefault(origin, route string) ([]byte, error) {
	pageURL, err := livePageURL(origin, route)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 1500 * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 {
				return nil
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirect left %s", via[0].URL.Host)
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Get(pageURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return nil, fmt.Errorf("content type %s", ct)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

func livePageURL(origin, route string) (string, error) {
	base, err := url.Parse(origin)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return "", fmt.Errorf("origin is not an http URL")
	}
	ref, err := url.Parse(route)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

func tagChain(n *html.Node) []string {
	var tags []string
	for cur := n; cur != nil && cur.Type == html.ElementNode; cur = cur.Parent {
		tags = append(tags, strings.ToUpper(cur.Data))
	}
	for i, j := 0, len(tags)-1; i < j; i, j = i+1, j-1 {
		tags[i], tags[j] = tags[j], tags[i]
	}
	return tags
}

func renderNode(n *html.Node) string {
	var b strings.Builder
	if err := html.Render(&b, n); err != nil {
		return ""
	}
	return b.String()
}

func accessibleName(n *html.Node) string {
	if label := attr(n, "aria-label"); label != "" {
		return truncateRunes(strings.TrimSpace(label), 80)
	}
	return truncateRunes(textContent(n), 80)
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func roleFor(n *html.Node) string {
	if role := attr(n, "role"); role != "" {
		return role
	}
	return implicitRoles[strings.ToUpper(n.Data)]
}

func landmarkFor(n *html.Node) string {
	for cur := n.Parent; cur != nil && cur.Type == html.ElementNode; cur = cur.Parent {
		if !landmarkTags[cur.Data] {
			continue
		}
		if label := attr(cur, "aria-label"); label != "" {
			return label
		}
		return cur.Data
	}
	return ""
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func leafTagFromSelector(selector string) string {
	compound := stripPseudos(lastCompound(selector))
	if compound == "" {
		return ""
	}
	switch compound[0] {
	case '#', '.', '[', ':', '*':
		return ""
	}
	i := 0
	for i < len(compound) {
		c := compound[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return ""
	}
	return strings.ToUpper(compound[:i])
}

func lastCompound(selector string) string {
	depth := 0
	var quote byte
	lastSplit := 0
	for i := 0; i < len(selector); i++ {
		c := selector[i]
		if quote != 0 {
			if c == quote && (i == 0 || selector[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case '>', '+', '~', ' ', '\t', '\n':
			if depth == 0 {
				lastSplit = i + 1
			}
		}
	}
	return strings.TrimSpace(selector[lastSplit:])
}

func stripPseudos(compound string) string {
	depth := 0
	for i := 0; i < len(compound); i++ {
		switch compound[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return compound[:i]
			}
		}
	}
	return compound
}

var landmarkTags = map[string]bool{
	"main": true, "nav": true, "header": true, "footer": true, "section": true, "aside": true,
}

var implicitRoles = map[string]string{
	"A": "link", "AREA": "link", "BUTTON": "button",
	"NAV": "navigation", "MAIN": "main", "HEADER": "banner", "FOOTER": "contentinfo",
	"ASIDE": "complementary", "SECTION": "region", "ARTICLE": "article",
	"H1": "heading", "H2": "heading", "H3": "heading", "H4": "heading", "H5": "heading", "H6": "heading",
	"UL": "list", "OL": "list", "LI": "listitem", "IMG": "img",
	"INPUT": "textbox", "SELECT": "combobox", "TEXTAREA": "textbox", "FORM": "form",
	"TABLE": "table", "TR": "row", "TH": "columnheader", "TD": "cell", "DIALOG": "dialog",
}
