package session

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

// PreviewEntryPath returns the share-payload path for a previewed HTML file.
// A relative htmlPath is kept as given (cleaned, forward-slash); an absolute
// one is made relative to baseDir (the working directory when baseDir is "").
// Paths that escape that tree fall back to the basename. The same value is
// sent as the review title (cli_args) and used as the crawled entry key, so
// the two can't drift. Comments authored on the preview are re-keyed to it
// when built into a share payload (see remapPreviewCommentFiles) — otherwise
// crit-web has no matching file to attach them to.
func PreviewEntryPath(htmlPath, baseDir string) string {
	p := filepath.Clean(htmlPath)
	if filepath.IsAbs(p) {
		if baseDir == "" {
			// On a Getwd error baseDir stays "", Rel fails, and relWithin
			// returns "..", so the basename fallback below applies.
			baseDir, _ = os.Getwd()
		}
		p = relWithin(baseDir, p)
	}
	p = filepath.ToSlash(p)
	// A leading "/" is left by a Windows root-relative path (\dir\a.html),
	// which filepath.IsAbs does not treat as absolute.
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return filepath.Base(htmlPath)
	}
	return p
}

// relWithin returns target relative to base. If that escapes base, it retries
// with symlinks resolved on both sides (macOS /var → /private/var: os.Getwd
// and a caller-supplied path can disagree on the prefix).
func relWithin(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	rb, errB := filepath.EvalSymlinks(base)
	rt, errT := filepath.EvalSymlinks(target)
	if errB != nil || errT != nil {
		return ".."
	}
	if rel, err := filepath.Rel(rb, rt); err == nil {
		return rel
	}
	return ".."
}

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
	baseDir string
	// keyDir is the directory of the entry HTML in the share payload. Assets are
	// read from baseDir+rel but stored under keyDir+rel, so the HTML's relative
	// refs still resolve when crit-web serves it from its original path.
	keyDir     string
	files      []ShareFile
	seen       map[string]bool
	totalBytes int
}

func newPreviewCollector(baseDir, keyDir string) *previewCollector {
	return &previewCollector{
		baseDir: baseDir,
		keyDir:  keyDir,
		seen:    map[string]bool{},
	}
}

// add stores data under key and marks rel (the path relative to baseDir) seen.
func (c *previewCollector) add(rel, key string, data []byte) error {
	c.totalBytes += len(data)
	if c.totalBytes > maxPreviewBytes {
		return fmt.Errorf("preview snapshot exceeds %dMB limit", maxPreviewBytes/(1024*1024))
	}
	c.files = append(c.files, makeShareFile(key, data))
	c.seen[rel] = true
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
	if err := c.add(rel, path.Join(c.keyDir, rel), data); err != nil {
		return false, err
	}
	return strings.HasSuffix(strings.ToLower(rel), ".css"), nil
}

// CrawlPreview reads an HTML file and all its local asset references,
// returning them as ShareFile entries suitable for uploading to crit-web.
// The HTML is the first entry, keyed entryPath (see PreviewEntryPath); assets
// are keyed relative to entryPath's directory. CSS files are followed one
// level deep to discover url() and @import refs. Missing assets are silently
// skipped. Total size is capped at maxPreviewBytes.
func CrawlPreview(htmlPath, entryPath string) ([]ShareFile, error) {
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	htmlData, err := os.ReadFile(absHTML)
	if err != nil {
		return nil, fmt.Errorf("read HTML: %w", err)
	}

	c := newPreviewCollector(filepath.Dir(absHTML), path.Dir(entryPath))

	if err := c.add(filepath.Base(absHTML), entryPath, htmlData); err != nil {
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

	// Web asset paths are always forward-slash (they become ShareFile keys and
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

// makeShareFile creates a ShareFile from raw bytes. Binary files are
// base64-encoded; text files are stored as UTF-8 strings.
func makeShareFile(relPath string, data []byte) ShareFile {
	ext := strings.ToLower(filepath.Ext(relPath))
	if textExtensions[ext] {
		return ShareFile{Path: relPath, Content: string(data)}
	}
	return ShareFile{
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
