package live

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

// agentScriptFiles lists JS injected into live/preview iframes (order matters).
var agentScriptFiles = []string{
	"agent-protocol.js",
	"agent-anchor-utils.js",
	"agent-scroll-utils.js",
	"agent-marker-overlay.js",
	"agent-mutation-batcher.js",
	"agent-resolution.js",
	"agent-reanchor-state.js",
	"crit-agent.js",
}

// newLiveProxy builds a reverse proxy for a live-mode session.
// upstreamOrigin is the target URL. Any path on it is the iframe's initial
// route, loaded by the frontend.
// apiPort is the API server's port, used to construct the agent script URLs
// on the host the browser used to reach the proxy.
func newLiveProxy(upstreamOrigin string, apiPort int, upstreamCookies string) (http.Handler, error) {
	target, err := url.Parse(upstreamOrigin)
	if err != nil {
		return nil, fmt.Errorf("parsing upstream origin %q: %w", upstreamOrigin, err)
	}
	// Keep only scheme+host so SetURL doesn't join path/query onto every request
	target.Path = ""
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""

	// Use a transport with DisableCompression=true so http.Transport does
	// not silently re-add Accept-Encoding: gzip after our Rewrite strips
	// it. Stripping matters because we need the upstream body uncompressed
	// in order to inject scripts.
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// SetURL sends the upstream's own Host, so tell the app where the
			// proxy is and let it generate URLs pointing back at us
			pr.SetXForwarded()
			if _, port, err := net.SplitHostPort(pr.In.Host); err == nil {
				pr.Out.Header.Set("X-Forwarded-Port", port)
			}
			pr.Out.Header.Del("Accept-Encoding")
			pr.Out.Header.Del("If-None-Match")
			pr.Out.Header.Del("If-Modified-Since")
			if upstreamCookies != "" {
				pr.Out.Header.Set("Cookie", upstreamCookies)
			}
			if pr.Out.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
			if ref := pr.Out.Header.Get("Referer"); ref != "" {
				pr.Out.Header.Set("Referer", rewriteReferer(ref, target))
			}
		},
		Transport:      transport,
		ModifyResponse: makeModifyResponse(apiPort, target),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Log the full error to stderr for debugging — it may include
			// the upstream URL or local file paths that we don't want to
			// echo back into a JSON envelope served to the browser.
			log.Printf("live proxy: upstream error for %s: %v", r.URL.Path, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":"upstream unreachable"}`)
		},
	}
	return rp, nil
}

var bodyTagRE = regexp.MustCompile(`(?i)</body>`)
var headTagRE = regexp.MustCompile(`(?i)<head[^>]*>`)

// maskHTMLComments returns a copy of body with the contents of every HTML
// comment (<!-- ... -->) overwritten with spaces. Length is preserved so
// indexes from regex matches on the masked copy are valid for the original.
// Used to keep <head>/</body> literals inside comments from misrouting
// injections. Unterminated comments mask to end-of-input.
func maskHTMLComments(body []byte) []byte {
	out := make([]byte, len(body))
	copy(out, body)
	for i := 0; i < len(out)-3; {
		if out[i] == '<' && out[i+1] == '!' && out[i+2] == '-' && out[i+3] == '-' {
			end := bytes.Index(out[i+4:], []byte("-->"))
			var stop int
			if end < 0 {
				stop = len(out)
			} else {
				stop = i + 4 + end + 3
			}
			for j := i; j < stop; j++ {
				out[j] = ' '
			}
			i = stop
			continue
		}
		i++
	}
	return out
}

// swShim runs once per top-level HTML document. It is injected as an inline
// <script> at the top of <head> so it executes before any page script can
// register a service worker.
//
// Two responsibilities:
//  1. Override navigator.serviceWorker.register to reject — blocks future
//     registrations from page scripts.
//  2. Unregister any existing service workers — a previous direct visit to
//     the upstream may have left a worker installed against the loopback
//     origin, and that worker would still intercept requests through the
//     proxy if not removed.
//
// JS response bodies are NOT modified; service workers are registered from
// HTML context, so neutering navigator there is sufficient and avoids
// breaking app code that imports JS modules.
const swShim = `<script>
(function () {
  if (typeof navigator === "undefined" || !navigator.serviceWorker) return;
  navigator.serviceWorker.register = function () {
    return Promise.reject(new Error("crit: service workers disabled"));
  };
  if (typeof navigator.serviceWorker.getRegistrations === "function") {
    navigator.serviceWorker.getRegistrations()
      .then(function (rs) { rs.forEach(function (r) { r.unregister(); }); })
      .catch(function () {});
  }
})();
</script>`

// maxHTMLBodyBytes caps how much of an HTML response we'll buffer for
// rewriting. Above the cap the body is passed through untouched and the
// X-Crit-Agent-Injection header signals the chrome that the agent didn't
// inject. Protects the daemon against an upstream that streams a multi-GB
// text/html response.
const maxHTMLBodyBytes = 25 << 20

func makeModifyResponse(apiPort int, upstream *url.URL) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return rewriteRedirect(resp, upstream)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			// JS, JSON, images, etc. pass through untouched.
			return nil
		}
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Length")
		// Force chunked transfer so the http.Server does not auto-set
		// Content-Length on the rewritten body — the response payload
		// changes after our injections, so the upstream value is stale
		// and must not be reused.
		resp.ContentLength = -1
		resp.TransferEncoding = []string{"chunked"}
		rewriteCookies(resp)

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBodyBytes+1))
		resp.Body.Close()
		// Tolerate ErrUnexpectedEOF *only* if we already got a usable body —
		// some upstreams set Content-Length larger than the actual body. If
		// the read failed before any bytes arrived, surface a 502 via
		// ErrorHandler instead of serving a blank 200 (confusing UX). Other
		// mid-body errors (context.Canceled, net resets, ErrClosedPipe) are
		// always returned.
		if err != nil {
			if !errors.Is(err, io.ErrUnexpectedEOF) || len(body) == 0 {
				return err
			}
		}
		if len(body) > maxHTMLBodyBytes {
			// Oversized: pass through untouched, signal injection failure.
			resp.Header.Set("X-Crit-Agent-Injection", "skipped-oversized")
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}
		// SetXForwarded copied the browser's Host header into X-Forwarded-Host.
		apiOrigin, ok := agentAPIOrigin(resp.Request.Header.Get("X-Forwarded-Host"), apiPort)
		if !ok {
			resp.Header.Set("X-Crit-Agent-Injection", "failed")
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}
		body, agentInjected := applyHTMLInjections(body, apiOrigin)
		if !agentInjected {
			resp.Header.Set("X-Crit-Agent-Injection", "failed")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
}

// applyHTMLInjections rewrites an HTML response body in a single pass:
// SW shim + route announcer right after the first <head ...>, then the
// agent script bundle right before the LAST </body>. Returns the modified
// body and whether the agent bundle was actually injected (caller sets
// X-Crit-Agent-Injection: failed when false). apiOrigin must come from
// agentAPIOrigin, which validates it.
func applyHTMLInjections(body []byte, apiOrigin string) ([]byte, bool) {
	// Match <head> / </body> against a comment-masked copy so literals
	// inside <!-- ... --> don't misroute injections; indexes line up
	// with the original because masking preserves length. Both lookups
	// are done up front, then splices are applied back-to-front so the
	// earlier offset stays valid.
	masked := maskHTMLComments(body)
	preamble := swShim + routeAnnouncerScript
	var sb strings.Builder
	for _, f := range agentScriptFiles {
		fmt.Fprintf(&sb, `<script src="%s/%s"></script>`, apiOrigin, f)
	}
	tags := sb.String()

	// Agent bundle insertion point: LAST </body>. Last match avoids matching
	// `</body>` literals inside <script> string literals earlier in the doc.
	bodyMatches := bodyTagRE.FindAllIndex(masked, -1)
	agentInjected := len(bodyMatches) > 0
	headLoc := headTagRE.FindIndex(masked)

	// Splice agent bundle first (later offset), then preamble (earlier).
	if agentInjected {
		at := bodyMatches[len(bodyMatches)-1][0]
		out := make([]byte, 0, len(body)+len(tags))
		out = append(out, body[:at]...)
		out = append(out, []byte(tags)...)
		out = append(out, body[at:]...)
		body = out
	}

	// SW shim + route announcer: insert after first <head ...>, or prepend
	// if absent so scripts still run before inline page scripts.
	if headLoc != nil {
		at := headLoc[1]
		out := make([]byte, 0, len(body)+len(preamble))
		out = append(out, body[:at]...)
		out = append(out, []byte(preamble)...)
		out = append(out, body[at:]...)
		body = out
	} else {
		out := make([]byte, 0, len(body)+len(preamble))
		out = append(out, []byte(preamble)...)
		out = append(out, body...)
		body = out
	}
	return body, agentInjected
}

// routeAnnouncerScript posts the iframe's pathname to the parent on initial
// load, after pushState/replaceState, and on popstate.
const routeAnnouncerScript = `<script data-crit-route-announcer>
(function(){
  function post(){
    try { parent.postMessage({type:"route-change", pathname: location.pathname}, "*"); } catch(e){}
  }
  var _ps = history.pushState;
  history.pushState = function(){ var r = _ps.apply(this, arguments); post(); return r; };
  var _rs = history.replaceState;
  history.replaceState = function(){ var r = _rs.apply(this, arguments); post(); return r; };
  window.addEventListener("popstate", post);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", post);
  } else {
    post();
  }
})();
</script>`

// sameOriginRedirectHost reports whether locHost names the same origin as the
// upstream or forwarded proxy host. Loopback aliases (localhost, 127.0.0.1,
// ::1) normalize together so framework redirects that hardcode a different
// loopback literal still rewrite to a proxy-relative Location.
func sameOriginRedirectHost(locHost, upstreamHost, forwardedHost string) bool {
	if locHost == upstreamHost || (forwardedHost != "" && locHost == forwardedHost) {
		return true
	}
	locKey := redirectHostKey(locHost)
	if locKey == "" {
		return false
	}
	if locKey == redirectHostKey(upstreamHost) {
		return true
	}
	return forwardedHost != "" && locKey == redirectHostKey(forwardedHost)
}

func redirectHostKey(host string) string {
	if host == "" {
		return ""
	}
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		h = host
		port = ""
	}
	h = strings.ToLower(h)
	switch h {
	case "localhost", "127.0.0.1", "::1":
		h = "loopback"
	}
	if port == "" {
		return h
	}
	return net.JoinHostPort(h, port)
}

func rewriteRedirect(resp *http.Response, upstream *url.URL) error {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil
	}
	locURL, err := url.Parse(loc)
	if err != nil {
		return nil //nolint:nilerr // unparseable Location: leave as-is (best effort)
	}
	if locURL.Host == "" {
		return nil // relative — already proxy-relative
	}
	if sameOriginRedirectHost(locURL.Host, upstream.Host, resp.Request.Header.Get("X-Forwarded-Host")) {
		// Build a path-absolute Location. Clearing Scheme/Host and calling
		// String() is unsafe: a path starting with "//" (or leftover User)
		// becomes protocol-relative and leaves the proxy origin.
		path := locURL.EscapedPath()
		if path == "" {
			path = "/"
		}
		for strings.HasPrefix(path, "//") {
			path = "/" + strings.TrimLeft(path, "/")
		}
		loc := path
		if locURL.RawQuery != "" {
			loc += "?" + locURL.RawQuery
		}
		resp.Header.Set("Location", loc)
		return nil
	}
	// Cross-origin: replace with 200 postMessage stub.
	// Drain and close upstream's small redirect body so the underlying
	// connection isn't left in a confused state when we substitute.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	// json.Marshal escapes <, >, &, U+2028, U+2029 — safe to embed in <script>.
	urlJS, _ := json.Marshal(loc)
	urlHTML := html.EscapeString(loc)
	stub := fmt.Sprintf(`<!DOCTYPE html><html><body><script>
(function(){try{window.parent.postMessage({type:"cross-origin-redirect",url:%s},"*");}catch(e){}}());
</script><p>cross-origin-redirect to <a href="%s">%s</a></p></body></html>`,
		urlJS, urlHTML, urlHTML)
	resp.StatusCode = http.StatusOK
	resp.Status = "200 OK"
	resp.Header.Del("Location")
	resp.Header.Del("Content-Length")
	resp.ContentLength = int64(len(stub))
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp.Body = io.NopCloser(strings.NewReader(stub))
	return nil
}

// rewriteCookies makes upstream Set-Cookie headers usable on the proxy
// origin. It strips three attributes:
//
//   - Domain — drop entirely; let the cookie default to the proxy host. The
//     upstream's host doesn't match the proxy's, so the original Domain would
//     cause the browser to silently reject the cookie.
//
//   - Secure — drop. The proxy serves over plain http; cookies marked Secure
//     would be discarded. We never relay the cookie back upstream as
//     Secure-stripped.
//
//   - SameSite=None — replace with SameSite=Lax. SameSite=None requires
//     Secure (which we just stripped); browsers reject the combination.
//     Downgrading to Lax is correct for a same-origin proxy: the cookie
//     remains usable for top-level navigation in the iframe.
//
// The proxy binds the daemon's listen host: loopback by default, or a
// non-loopback --host acknowledged with --allow-unauthenticated-network.
// A --public-url can also expose a loopback listener through a reverse proxy.
// Each browser that reaches the proxy keeps its own rewritten cookies, but
// crit live refuses --cookie, --cookie-file and --cdp-url in either exposed
// setup so it never attaches the reviewer's session to visitors' requests.
func rewriteCookies(resp *http.Response) {
	cookies := resp.Header["Set-Cookie"]
	if len(cookies) == 0 {
		return
	}
	out := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts := strings.Split(c, ";")
		kept := parts[:1]
		for _, p := range parts[1:] {
			lower := strings.ToLower(strings.TrimSpace(p))
			switch {
			case strings.HasPrefix(lower, "domain="):
				continue
			case lower == "secure":
				continue
			case lower == "samesite=none":
				kept = append(kept, " SameSite=Lax")
				continue
			}
			kept = append(kept, p)
		}
		out = append(out, strings.Join(kept, ";"))
	}
	resp.Header["Set-Cookie"] = out
}

// BindProxyServer creates a TCP listener on listenHost:(apiPort+1), the same
// host as the API server (127.0.0.1 when empty), and returns a proxy server.
// The server is not yet started; caller calls srv.Serve(ln).
func BindProxyServer(listenHost, publicURL, upstreamOrigin string, apiPort int, upstreamCookies string) (net.Listener, *http.Server, error) {
	if listenHost == "" {
		listenHost = "127.0.0.1"
	}
	proxyPort := apiPort + 1
	ln, err := net.Listen("tcp", net.JoinHostPort(listenHost, strconv.Itoa(proxyPort)))
	if err != nil {
		return nil, nil, fmt.Errorf("proxy port %d already in use: %w", proxyPort, err)
	}
	proxy, err := newLiveProxy(upstreamOrigin, apiPort, upstreamCookies)
	if err != nil {
		ln.Close()
		return nil, nil, err
	}
	srv := &http.Server{
		Handler:     checkProxyHost(listenHost, config.PublicURLHost(publicURL), proxy),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	return ln, srv, nil
}

// checkProxyHost rejects malformed Host headers and, on a loopback listener,
// permits only loopback or the configured public URL host. This matches the
// API server's DNS-rebinding defense.
func checkProxyHost(listenHost, publicURLHost string, next http.Handler) http.Handler {
	loopbackOnly := config.IsLoopbackHost(listenHost)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := config.RequestHost(r.Host)
		publicHost := publicURLHost != "" && strings.EqualFold(host, publicURLHost)
		if !validProxyHost(host) || (loopbackOnly && !config.IsLoopbackHost(host) && !publicHost) {
			http.Error(w, fmt.Sprintf("Forbidden: request Host %q is not allowed on the live proxy", host), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validProxyHost reports whether host (no port) is an IP literal or a DNS
// name. It keeps request Host headers out of the injected HTML unless they
// are plain hostnames.
func validProxyHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}

// agentAPIOrigin returns the API server origin the injected scripts load
// from: the host the browser used to reach the proxy, on the API port. The
// review page reaches the proxy on its own hostname, so this is also the
// origin of the page that embeds the iframe.
func agentAPIOrigin(proxyHostport string, apiPort int) (string, bool) {
	host := config.RequestHost(proxyHostport)
	if !validProxyHost(host) {
		return "", false
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(apiPort)), true
}

// rewriteReferer swaps in the upstream origin but keeps the referring page's own path
// and query, which frameworks read to work out where "redirect back" should land.
// Path is built the same way as rewriteRedirect: never via url.URL.String(), and
// leading "//" is collapsed so a forged Referer cannot become protocol-relative
// when an app turns it into a Location.
func rewriteReferer(ref string, upstream *url.URL) string {
	origin := upstream.Scheme + "://" + upstream.Host
	refURL, err := url.Parse(ref)
	if err != nil {
		return origin
	}
	path := refURL.EscapedPath()
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for strings.HasPrefix(path, "//") {
		path = "/" + strings.TrimLeft(path, "/")
	}
	out := origin + path
	if refURL.RawQuery != "" {
		out += "?" + refURL.RawQuery
	}
	return out
}
