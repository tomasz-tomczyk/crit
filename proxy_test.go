package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyDirector_StripsAcceptEncoding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	req, _ := http.NewRequest("GET", ps.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; Accept-Encoding was not stripped", resp.StatusCode)
	}
}

func TestProxyDirector_PreservesCookieAndAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=abc" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	req, _ := http.NewRequest("GET", ps.URL+"/", nil)
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; Cookie/Authorization not forwarded", resp.StatusCode)
	}
}

func TestProxyDirector_SetsUpstreamHost(t *testing.T) {
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)

	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	resp, _ := http.Get(ps.URL + "/")
	if resp != nil {
		resp.Body.Close()
	}
	if gotHost != upstreamURL.Host {
		t.Errorf("Host = %q, want %q", gotHost, upstreamURL.Host)
	}
}

// Suppress unused
var _ = io.ReadAll
var _ = strings.Contains
var _ = net.Listen

func TestProxyModifyResponse_StripsSecurityHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Length", "50")
		fmt.Fprintln(w, "<html><body>app</body></html>")
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, _ := http.Get(ps.URL + "/")
	if resp != nil {
		resp.Body.Close()
	}
	if resp.Header.Get("Content-Security-Policy") != "" {
		t.Error("CSP not stripped")
	}
	if resp.Header.Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options not stripped")
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Error("Content-Length not stripped")
	}
}

func TestProxyModifyResponse_InjectsAgentBeforeBodyTag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body><p>Hello</p></body></html>")
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 54321)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bs := string(body)
	if !strings.Contains(bs, "http://localhost:54321/crit-agent.js") {
		t.Errorf("agent tag not injected: %s", bs)
	}
	if !strings.Contains(bs, "http://localhost:54321/crit-vendor/html2canvas.js") {
		t.Errorf("html2canvas tag not injected: %s", bs)
	}
	hci := strings.Index(bs, "html2canvas.js")
	ai := strings.Index(bs, "crit-agent.js")
	bi := strings.Index(bs, "</body>")
	if hci < 0 || ai < 0 || bi < 0 {
		t.Fatalf("missing tags or </body>: html2canvas=%d agent=%d body=%d", hci, ai, bi)
	}
	if hci > ai {
		t.Errorf("html2canvas must come before crit-agent: html2canvas=%d agent=%d", hci, ai)
	}
	if ai > bi {
		t.Errorf("agent tag not before </body>")
	}
}

func TestProxyModifyResponse_SkipsInjectionWhenNoBodyTag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><!-- no body tag -->")
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 even without </body>", resp.StatusCode)
	}
	if resp.Header.Get("X-Crit-Agent-Injection") != "failed" {
		t.Errorf("X-Crit-Agent-Injection header not set to 'failed'")
	}
}

func TestProxyModifyResponse_SameOriginRedirectRewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>dashboard</body></html>")
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := client.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, upURL.Host) {
		t.Errorf("Location still points to upstream: %s", loc)
	}
}

func TestProxyModifyResponse_CrossOriginRedirect200Stub(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://accounts.google.com/oauth", http.StatusFound)
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := client.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 stub", resp.StatusCode)
	}
	if !strings.Contains(string(body), "cross-origin-redirect") {
		t.Errorf("missing postMessage stub: %s", body)
	}
}

func TestProxyModifyResponse_StripsCookieDomain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Set-Cookie", "foo=bar; Domain=upstream.test; Path=/; HttpOnly")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, _ := http.Get(ps.URL + "/")
	if resp != nil {
		resp.Body.Close()
	}
	sc := resp.Header.Get("Set-Cookie")
	if strings.Contains(strings.ToLower(sc), "domain=") {
		t.Errorf("Domain attribute not stripped: %s", sc)
	}
	if !strings.Contains(sc, "foo=bar") {
		t.Errorf("cookie value lost: %s", sc)
	}
}

func TestProxyModifyResponse_SWShimInjectedInHTMLHead(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><head><title>app</title></head><body><script>navigator.serviceWorker.register('/sw.js');</script></body></html>`)
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bs := string(body)
	if !strings.Contains(bs, "crit: service workers disabled") {
		t.Errorf("SW shim not injected: %s", bs)
	}
	headIdx := strings.Index(bs, "<head")
	shimIdx := strings.Index(bs, "crit: service workers disabled")
	firstScriptInBody := strings.Index(bs, "<body")
	if headIdx < 0 || shimIdx < headIdx {
		t.Errorf("shim not after <head>: head=%d shim=%d", headIdx, shimIdx)
	}
	if shimIdx > firstScriptInBody {
		t.Errorf("shim must execute before body scripts: shim=%d body=%d", shimIdx, firstScriptInBody)
	}
}

func TestProxyModifyResponse_JSResponsesPassThrough(t *testing.T) {
	js := "navigator.serviceWorker.register('/sw.js');"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/app.js")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != js {
		t.Errorf("JS body modified — must be untouched\n got=%q\nwant=%q", body, js)
	}
}

func TestProxyModifyResponse_NonHTMLPassedThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "preserved")
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	proxy, _ := newDesignProxy(upstream.URL, 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/api/data")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("X-Custom") != "preserved" {
		t.Errorf("non-HTML header modified")
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Errorf("non-HTML body modified: %s", body)
	}
}

func TestProxyErrorHandler_Returns502JSON(t *testing.T) {
	proxy, _ := newDesignProxy("http://127.0.0.1:19998", 9001)
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"error"`) {
		t.Errorf("expected JSON error body: %s", body)
	}
}

func TestBindProxyServer_PortIsAPIPlusOne(t *testing.T) {
	ln0, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiPort := ln0.Addr().(*net.TCPAddr).Port
	ln0.Close()

	ln, srv, err := bindProxyServer("http://127.0.0.1:19997", apiPort)
	if err != nil {
		t.Fatalf("bindProxyServer: %v", err)
	}
	defer ln.Close()
	_ = srv
	if ln.Addr().(*net.TCPAddr).Port != apiPort+1 {
		t.Errorf("proxy port = %d, want %d", ln.Addr().(*net.TCPAddr).Port, apiPort+1)
	}
}

func TestProxyModifyResponse_InjectsRouteAnnouncer(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"head present", `<!doctype html><html><head><title>x</title></head><body>hi</body></html>`, true},
		{"head with attrs", `<html><head lang="en" class="dark"><meta charset="utf-8"></head><body></body></html>`, true},
		{"no head tag", `<html><body>hi</body></html>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()

			handler, err := newDesignProxy(upstream.URL, 4101)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if !strings.Contains(body, "data-crit-route-announcer") {
				t.Errorf("expected announcer marker; got body=%q", body)
			}
			if !strings.Contains(body, "route-change") {
				t.Errorf("expected announcer to post 'route-change'; got body=%q", body)
			}
			if !strings.Contains(body, "pushState") || !strings.Contains(body, "replaceState") || !strings.Contains(body, "popstate") {
				t.Errorf("expected announcer to wrap pushState/replaceState and listen for popstate; got body=%q", body)
			}
		})
	}
}
