package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// newDesignProxy builds a reverse proxy for a design-mode session.
// upstreamOrigin is the target scheme+host+port (e.g. "http://localhost:3000").
// apiPort is the API server's port, used to construct the agent script URL.
func newDesignProxy(upstreamOrigin string, apiPort int) (http.Handler, error) {
	target, err := url.Parse(upstreamOrigin)
	if err != nil {
		return nil, fmt.Errorf("parsing upstream origin %q: %w", upstreamOrigin, err)
	}

	// Use a transport with DisableCompression=true so http.Transport does
	// not silently re-add Accept-Encoding: gzip after our Director strips
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
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.Header.Del("Accept-Encoding")
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		},
		Transport:      transport,
		ModifyResponse: makeModifyResponse(apiPort, target),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":"upstream unreachable","detail":%q}`, err.Error())
		},
	}
	return rp, nil
}

var bodyTagRE = regexp.MustCompile(`(?i)</body>`)
var headTagRE = regexp.MustCompile(`(?i)<head[^>]*>`)

// swShim runs once per top-level HTML document. It is injected as an inline
// <script> at the top of <head> so it executes before any page script can
// register a service worker. JS response bodies are NOT modified — service
// workers are registered from HTML context, so neutering navigator there is
// sufficient and avoids breaking app code that imports JS modules.
const swShim = `<script>if(typeof navigator!=="undefined"&&navigator.serviceWorker){navigator.serviceWorker.register=function(){return Promise.reject(new Error("crit: service workers disabled"));};}</script>`

func makeModifyResponse(apiPort int, upstream *url.URL) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return rewriteRedirect(resp, upstream)
		}
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/html") {
			resp.Header.Del("Content-Security-Policy")
			resp.Header.Del("X-Frame-Options")
			resp.Header.Del("Content-Length")
			stripCookieDomain(resp)
			if err := injectSWShimHTML(resp); err != nil {
				return err
			}
			return injectAgentScript(resp, apiPort)
		}
		// JS, JSON, images, etc. pass through untouched.
		return nil
	}
}

func injectAgentScript(resp *http.Response, apiPort int) error {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	// Order matters: html2canvas must load before crit-agent so the agent
	// can use it during pin capture. Both go before </body>.
	tags := fmt.Sprintf(
		`<script src="http://localhost:%d/crit-vendor/html2canvas.js"></script>`+
			`<script src="http://localhost:%d/crit-agent.js"></script>`,
		apiPort, apiPort,
	)
	if !bodyTagRE.Match(body) {
		resp.Header.Set("X-Crit-Agent-Injection", "failed")
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	injected := bodyTagRE.ReplaceAllFunc(body, func(match []byte) []byte {
		return []byte(tags + string(match))
	})
	resp.Body = io.NopCloser(bytes.NewReader(injected))
	return nil
}

// injectSWShimHTML inserts an inline <script> immediately after <head ...>
// so the service-worker neutering executes before any page script. If no
// <head> tag is present (rare), the shim is prepended to the body so it
// still runs before inline scripts further down the document.
func injectSWShimHTML(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	if loc := headTagRE.FindIndex(body); loc != nil {
		out := make([]byte, 0, len(body)+len(swShim))
		out = append(out, body[:loc[1]]...)
		out = append(out, []byte(swShim)...)
		out = append(out, body[loc[1]:]...)
		resp.Body = io.NopCloser(bytes.NewReader(out))
		return nil
	}
	resp.Body = io.NopCloser(io.MultiReader(
		strings.NewReader(swShim),
		bytes.NewReader(body),
	))
	return nil
}

func rewriteRedirect(resp *http.Response, upstream *url.URL) error {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return nil
	}
	locURL, err := url.Parse(loc)
	if err != nil || locURL.Host == "" {
		return nil // relative — already proxy-relative
	}
	if locURL.Host == upstream.Host {
		locURL.Scheme = resp.Request.URL.Scheme
		locURL.Host = resp.Request.URL.Host
		resp.Header.Set("Location", locURL.String())
		return nil
	}
	// Cross-origin: replace with 200 postMessage stub.
	stub := fmt.Sprintf(`<!DOCTYPE html><html><body><script>
(function(){try{window.parent.postMessage({type:"cross-origin-redirect",url:%q},"*");}catch(e){}}());
</script><p>cross-origin-redirect to <a href=%q>%s</a></p></body></html>`,
		loc, loc, loc)
	resp.StatusCode = http.StatusOK
	resp.Status = "200 OK"
	resp.Header.Del("Location")
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp.Body = io.NopCloser(strings.NewReader(stub))
	return nil
}

func stripCookieDomain(resp *http.Response) {
	cookies := resp.Header["Set-Cookie"]
	if len(cookies) == 0 {
		return
	}
	out := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts := strings.Split(c, ";")
		kept := parts[:1]
		for _, p := range parts[1:] {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p)), "domain=") {
				continue
			}
			kept = append(kept, p)
		}
		out = append(out, strings.Join(kept, ";"))
	}
	resp.Header["Set-Cookie"] = out
}

// bindProxyServer creates a TCP listener on 127.0.0.1:(apiPort+1) and
// returns (listener, *http.Server, error). The server is not yet started;
// caller calls srv.Serve(ln).
func bindProxyServer(upstreamOrigin string, apiPort int) (net.Listener, *http.Server, error) {
	proxyPort := apiPort + 1
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort))
	if err != nil {
		return nil, nil, fmt.Errorf("proxy port %d already in use: %w", proxyPort, err)
	}
	handler, err := newDesignProxy(upstreamOrigin, apiPort)
	if err != nil {
		ln.Close()
		return nil, nil, err
	}
	srv := &http.Server{
		Handler:     handler,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	return ln, srv, nil
}
