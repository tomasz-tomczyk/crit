package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

// The review page renders Markdown and comments that an AI agent may have
// written. A remote image in that content would make the reviewer's browser
// send a request, with whatever the URL encodes, to a host the agent chose:
// a way out of a network sandbox the agent itself cannot use. The page
// therefore ships a Content-Security-Policy that blocks remote images and
// media except for the hosts the user lists in remote_images (global config).
// The frontend (web/crit-remote-images.js) shows a click-to-load placeholder
// for every other remote image; a click loads it in a sandboxed frame served
// by /remote-image, which has its own policy.

// remoteImagesMeta is the empty tag in index.html that serveShell fills with
// the remote_images allowlist, so the frontend matches the policy without
// waiting for /api/config.
const remoteImagesMeta = `<meta name="crit-remote-images" content="[]">`

var (
	inlineScriptRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	scriptSrcRe    = regexp.MustCompile(`(?i)\bsrc\s*=`)
)

// inlineScriptHashes returns CSP 'sha256-…' sources for every inline
// <script> (one without a src attribute) in page, so the shell's inline
// bootstrap keeps running under script-src without 'unsafe-inline'. The hash
// covers the script's text only, so its attributes don't matter.
func inlineScriptHashes(page []byte) []string {
	var out []string
	for _, m := range inlineScriptRe.FindAllSubmatch(page, -1) {
		if scriptSrcRe.Match(m[1]) {
			continue
		}
		sum := sha256.Sum256(m[2])
		out = append(out, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return out
}

// remoteImageSources maps remote_images patterns to CSP host sources. "*"
// allows every http(s) host. Patterns were validated when the config loaded;
// anything else is dropped again here because it lands in a header.
func remoteImageSources(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if p == "*" {
			return []string{"http:", "https:"}
		}
		if config.ValidRemoteImagePattern(p) {
			out = append(out, p)
		}
	}
	return out
}

// reviewPageCSP builds the policy for the review page shells (index.html at
// /, /live and /preview; themes.html).
func reviewPageCSP(scriptHashes, remoteImages []string) string {
	remote := remoteImageSources(remoteImages)
	imgSrc := append([]string{"'self'", "data:", "blob:"}, remote...)
	mediaSrc := append([]string{"'self'", "data:", "blob:"}, remote...)
	scriptSrc := append([]string{"'self'", "'wasm-unsafe-eval'"}, scriptHashes...)
	directives := []string{
		"default-src 'self'",
		"script-src " + strings.Join(scriptSrc, " "),
		"worker-src 'self' blob:",
		// Shiki tokens, mermaid SVGs and crit's own markup use inline styles.
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' data: https://fonts.gstatic.com",
		"img-src " + strings.Join(imgSrc, " "),
		"media-src " + strings.Join(mediaSrc, " "),
		"connect-src 'self'",
		// Live mode frames the user's dev server through its proxy, and the
		// framed app may navigate anywhere (links, SSO). frame-src covers every
		// navigation in that frame, so it can't be narrowed to the proxy. The
		// sanitizers drop <iframe> from rendered content, so agent-written
		// content can't add a frame.
		"frame-src 'self' http: https:",
		"form-action 'self'",
		"object-src 'none'",
		"base-uri 'self'",
	}
	return strings.Join(directives, "; ")
}

// injectRemoteImagesMeta fills index.html's crit-remote-images meta tag with
// the allowlist as JSON.
func injectRemoteImagesMeta(page []byte, remoteImages []string) []byte {
	if remoteImages == nil {
		remoteImages = []string{}
	}
	data, err := json.Marshal(remoteImages)
	if err != nil {
		return page
	}
	tag := `<meta name="crit-remote-images" content="` + html.EscapeString(string(data)) + `">`
	return bytes.Replace(page, []byte(remoteImagesMeta), []byte(tag), 1)
}

// serveShell writes an embedded HTML shell with the review page's CSP.
func (s *Server) serveShell(w http.ResponseWriter, name string) {
	f, err := s.assets.Open(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	page, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	remoteImages := s.cfg.RemoteImages
	if name == "index.html" {
		page = injectRemoteImagesMeta(page, remoteImages)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", reviewPageCSP(inlineScriptHashes(page), remoteImages))
	w.Header().Set("Referrer-Policy", "no-referrer")
	// CSP doesn't cover DNS: without this, browsers may resolve the host of
	// every link on the page, and an agent could put data in a hostname.
	w.Header().Set("X-DNS-Prefetch-Control", "off")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

// remoteImageFrameScript reports the image's size (or failure) to the review
// page so it can size the frame. It is the frame's only script, allowed by
// hash.
const remoteImageFrameScript = `(function () {
  var img = document.getElementById('img');
  function send(status) {
    parent.postMessage({ type: 'crit-remote-image', status: status, width: img.naturalWidth, height: img.naturalHeight }, '*');
  }
  if (img.complete) {
    send(img.naturalWidth ? 'loaded' : 'error');
    return;
  }
  img.addEventListener('load', function () { send('loaded'); });
  img.addEventListener('error', function () { send('error'); });
})();`

var remoteImageFrameScriptHash = func() string {
	sum := sha256.Sum256([]byte(remoteImageFrameScript))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

// remoteImageFrameCSP lets the frame load one remote image and nothing else.
// It is only ever framed by the review page, which loads it in a sandboxed
// iframe after the reviewer clicks a placeholder. The sandbox directive gives
// it an opaque origin even when it is opened some other way.
var remoteImageFrameCSP = strings.Join([]string{
	"sandbox allow-scripts",
	"default-src 'none'",
	"img-src http: https:",
	"style-src 'unsafe-inline'",
	"script-src " + remoteImageFrameScriptHash,
	"base-uri 'none'",
	"form-action 'none'",
	"frame-ancestors 'self'",
}, "; ")

const maxRemoteImageURL = 8 << 10

// handleRemoteImage serves the click-to-load frame for one remote image:
// GET /remote-image?src=<http(s) URL>&alt=<text>.
func (s *Server) handleRemoteImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	src := r.URL.Query().Get("src")
	if len(src) > maxRemoteImageURL {
		http.Error(w, "src too long", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "src must be an http(s) URL", http.StatusBadRequest)
		return
	}
	alt := r.URL.Query().Get("alt")
	if len(alt) > 1024 {
		alt = strings.ToValidUTF8(alt[:1024], "")
	}
	page := `<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer">` +
		`<style>html,body{margin:0;padding:0;background:transparent;overflow:hidden}` +
		`img{display:block;width:100%;height:100%;object-fit:contain}</style></head><body>` +
		`<img id="img" src="` + html.EscapeString(u.String()) + `" alt="` + html.EscapeString(alt) + `">` +
		`<script>` + remoteImageFrameScript + `</script></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", remoteImageFrameCSP)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, page)
}
