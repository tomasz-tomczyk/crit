package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const maxPreviewBytes = 10 * 1024 * 1024 // 10MB total snapshot limit

// previewMainHTMLKey is the share-payload path under which the previewed HTML
// file is stored. The crawler roots the snapshot here and assets hang off it by
// their relative paths. Comments authored on the preview are stored under the
// session's on-disk path, so they must be re-keyed to this constant when built
// into a share payload (see remapPreviewCommentFiles) — otherwise crit-web has
// no matching file to attach them to. Single constant so the two can't drift.
const previewMainHTMLKey = "index.html"

// textExtensions lists file extensions served as plain text (not base64).
var textExtensions = map[string]bool{
	".html": true,
	".htm":  true,
	".css":  true,
	".js":   true,
	".json": true,
	".svg":  true,
	".xml":  true,
	".txt":  true,
	".map":  true,
	".mjs":  true,
}

var (
	cssURLRe    = regexp.MustCompile(`url\(\s*['"]?([^'")\s]+)['"]?\s*\)`)
	cssImportRe = regexp.MustCompile(`@import\s+['"]([^'"]+)['"]`)
)

// previewCollector accumulates files for a preview snapshot, tracking
// seen paths and enforcing the total size limit.
type previewCollector struct {
	baseDir    string
	files      []shareFile
	seen       map[string]bool
	totalBytes int
}

func newPreviewCollector(baseDir string) *previewCollector {
	return &previewCollector{
		baseDir: baseDir,
		seen:    map[string]bool{},
	}
}

func (c *previewCollector) add(relPath string, data []byte) error {
	c.totalBytes += len(data)
	if c.totalBytes > maxPreviewBytes {
		return fmt.Errorf("preview snapshot exceeds %dMB limit", maxPreviewBytes/(1024*1024))
	}
	c.files = append(c.files, makeShareFile(relPath, data))
	c.seen[relPath] = true
	return nil
}

// tryAdd reads a file from disk relative to baseDir and adds it to the
// collection. Returns true if the file was a CSS file (for further crawling).
// Missing files are silently skipped.
func (c *previewCollector) tryAdd(rel string) (isCSS bool, err error) {
	data, readErr := os.ReadFile(filepath.Join(c.baseDir, rel))
	if readErr != nil {
		return false, nil //nolint:nilerr // missing assets are intentionally skipped
	}
	if err := c.add(rel, data); err != nil {
		return false, err
	}
	return strings.HasSuffix(strings.ToLower(rel), ".css"), nil
}

// crawlPreview reads an HTML file and all its local asset references,
// returning them as shareFile entries suitable for uploading to crit-web.
// CSS files are followed one level deep to discover url() and @import refs.
// Missing assets are silently skipped. Total size is capped at maxPreviewBytes.
func crawlPreview(htmlPath string) ([]shareFile, error) {
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	htmlData, err := os.ReadFile(absHTML)
	if err != nil {
		return nil, fmt.Errorf("read HTML: %w", err)
	}

	c := newPreviewCollector(filepath.Dir(absHTML))

	if err := c.add(previewMainHTMLKey, htmlData); err != nil {
		return nil, err
	}

	cssFiles, err := collectHTMLAssets(c, htmlData)
	if err != nil {
		return nil, err
	}

	if err := collectCSSAssets(c, cssFiles); err != nil {
		return nil, err
	}

	return c.files, nil
}

// collectHTMLAssets reads all assets referenced in the HTML and adds them to
// the collector. Returns the list of CSS relative paths for further crawling.
func collectHTMLAssets(c *previewCollector, htmlData []byte) ([]string, error) {
	var cssFiles []string
	for _, ref := range extractHTMLRefs(htmlData) {
		if isExternalURL(ref) {
			continue
		}
		rel := cleanRelPath(ref)
		if rel == "" || c.seen[rel] {
			continue
		}
		isCSS, err := c.tryAdd(rel)
		if err != nil {
			return nil, err
		}
		if isCSS {
			cssFiles = append(cssFiles, rel)
		}
	}
	return cssFiles, nil
}

// collectCSSAssets follows CSS files one level deep, reading url() and @import
// references and adding them to the collector.
func collectCSSAssets(c *previewCollector, cssFiles []string) error {
	for _, cssRel := range cssFiles {
		cssData, readErr := os.ReadFile(filepath.Join(c.baseDir, cssRel))
		if readErr != nil {
			continue
		}
		cssDir := path.Dir(cssRel)
		for _, ref := range extractCSSURLs(string(cssData)) {
			if isExternalURL(ref) {
				continue
			}
			rel := resolveCSSRef(cssDir, ref)
			if rel == "" || c.seen[rel] {
				continue
			}
			if _, err := c.tryAdd(rel); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveCSSRef cleans a CSS reference path and resolves it relative to the
// CSS file's directory. Returns empty string for invalid paths.
func resolveCSSRef(cssDir, ref string) string {
	rel := cleanRelPath(ref)
	if rel == "" {
		return ""
	}
	if cssDir != "." {
		rel = path.Join(cssDir, rel)
	}
	return path.Clean(rel)
}

// extractHTMLRefs parses HTML and returns local asset paths referenced by
// link[rel=stylesheet], script[src], img[src], and source[src/srcset].
func extractHTMLRefs(data []byte) []string {
	var refs []string
	z := html.NewTokenizer(bytes.NewReader(data))

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}

		tn, hasAttr := z.TagName()
		if !hasAttr {
			continue
		}
		tag := string(tn)

		switch tag {
		case "link":
			refs = append(refs, extractLinkRefs(z)...)
		case "script":
			if src := attrVal(z, "src"); src != "" {
				refs = append(refs, src)
			}
		case "img":
			if src := attrVal(z, "src"); src != "" {
				refs = append(refs, src)
			}
		case "source":
			refs = append(refs, extractSourceRefs(z)...)
		}
	}

	return refs
}

// extractLinkRefs returns href from a <link> tag if rel=stylesheet.
func extractLinkRefs(z *html.Tokenizer) []string {
	var href, rel string
	for {
		key, val, more := z.TagAttr()
		k := string(key)
		if k == "href" {
			href = string(val)
		}
		if k == "rel" {
			rel = string(val)
		}
		if !more {
			break
		}
	}
	if rel == "stylesheet" && href != "" {
		return []string{href}
	}
	return nil
}

// extractSourceRefs returns src and srcset values from a <source> tag.
func extractSourceRefs(z *html.Tokenizer) []string {
	var refs []string
	for {
		key, val, more := z.TagAttr()
		k := string(key)
		if k == "src" && len(val) > 0 {
			refs = append(refs, string(val))
		}
		if k == "srcset" && len(val) > 0 {
			refs = append(refs, parseSrcset(string(val))...)
		}
		if !more {
			break
		}
	}
	return refs
}

// attrVal returns the value of the named attribute from the current token.
// It consumes all remaining attributes.
func attrVal(z *html.Tokenizer, name string) string {
	var result string
	for {
		key, val, more := z.TagAttr()
		if string(key) == name {
			result = string(val)
		}
		if !more {
			break
		}
	}
	return result
}

// parseSrcset splits an HTML srcset attribute into individual URLs.
func parseSrcset(srcset string) []string {
	var urls []string
	for _, entry := range strings.Split(srcset, ",") {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) > 0 {
			urls = append(urls, parts[0])
		}
	}
	return urls
}

// extractCSSURLs returns all local paths referenced via url() or @import in CSS.
func extractCSSURLs(css string) []string {
	var refs []string

	for _, m := range cssURLRe.FindAllStringSubmatch(css, -1) {
		if len(m) > 1 && m[1] != "" {
			refs = append(refs, m[1])
		}
	}
	for _, m := range cssImportRe.FindAllStringSubmatch(css, -1) {
		if len(m) > 1 && m[1] != "" {
			refs = append(refs, m[1])
		}
	}

	return refs
}

// cleanRelPath normalizes a reference path: strips query/fragment, rejects
// absolute paths, parent traversal, and data URIs. Returns empty string for invalid paths.
func cleanRelPath(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "data:") {
		return ""
	}

	// Strip query string and fragment.
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}

	// Reject absolute paths.
	if strings.HasPrefix(p, "/") {
		return ""
	}

	// Web asset paths are always forward-slash (they become shareFile keys and
	// must match HTML/CSS refs + crit-web's served paths), so clean with `path`,
	// not `filepath` — the latter yields backslashes on Windows. Disk reads in
	// tryAdd still join via filepath, which accepts these forward-slash rels.
	p = path.Clean(p)

	// Reject parent traversal.
	if strings.HasPrefix(p, "..") {
		return ""
	}

	return p
}

// makeShareFile creates a shareFile from raw bytes. Binary files are
// base64-encoded; text files are stored as UTF-8 strings.
func makeShareFile(relPath string, data []byte) shareFile {
	ext := strings.ToLower(filepath.Ext(relPath))
	if textExtensions[ext] {
		return shareFile{Path: relPath, Content: string(data)}
	}
	return shareFile{
		Path:     relPath,
		Content:  base64.StdEncoding.EncodeToString(data),
		Encoding: "base64",
	}
}

// isExternalURL returns true for URLs that point to external resources
// (http://, https://, protocol-relative //, or data URIs).
func isExternalURL(ref string) bool {
	return strings.HasPrefix(ref, "http://") ||
		strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "//") ||
		strings.HasPrefix(ref, "data:")
}
