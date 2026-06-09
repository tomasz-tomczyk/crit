package main

import (
	"bytes"
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

	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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

func TestProxyDirector_InjectsConfiguredCookies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	proxy, _ := newLiveProxy(upstream.URL, 9001, "session=abc")
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; configured cookie was not injected", resp.StatusCode)
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

	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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

	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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

func TestProxyModifyResponse_StripsSecurityHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Length", "50")
		fmt.Fprintln(w, "<html><body>app</body></html>")
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy(upstream.URL, 54321, "")
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
	if strings.Contains(bs, "html2canvas") {
		t.Errorf("html2canvas should not be injected: %s", bs)
	}
	if !strings.Contains(bs, "http://localhost:54321/agent-protocol.js") {
		t.Errorf("protocol tag not injected: %s", bs)
	}
	if !strings.Contains(bs, "http://localhost:54321/agent-anchor-utils.js") {
		t.Errorf("anchor-utils tag not injected: %s", bs)
	}
	pi := strings.Index(bs, "/agent-protocol.js")
	ui := strings.Index(bs, "/agent-anchor-utils.js")
	ai := strings.Index(bs, "/crit-agent.js")
	bi := strings.Index(bs, "</body>")
	if pi < 0 || ui < 0 || ai < 0 || bi < 0 {
		t.Fatalf("missing tags or </body>: protocol=%d utils=%d agent=%d body=%d", pi, ui, ai, bi)
	}
	if !(pi < ui && ui < ai) {
		t.Errorf("expected protocol < utils < agent, got pi=%d ui=%d ai=%d", pi, ui, ai)
	}
	if ai > bi {
		t.Errorf("agent tag not before </body>")
	}
}

func TestProxyModifyResponse_InjectsAtLastBodyTag(t *testing.T) {
	// Simulates a page where </body> appears inside a string literal in a
	// <script>. The agent bundle must inject before the LAST </body> (the
	// real document terminator), not the first one (inside the script).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><body><script>var s = "</body>";</script><p>Real content</p></body></html>`)
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 54321, "")
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bs := string(body)
	// Find first </body> (inside string literal) and last </body> (real).
	first := strings.Index(bs, "</body>")
	last := strings.LastIndex(bs, "</body>")
	agent := strings.Index(bs, "/crit-agent.js")
	if first == last {
		t.Fatalf("expected two </body> occurrences, got one at %d", first)
	}
	if agent < first {
		t.Errorf("agent inserted before first (string-literal) </body> at %d (agent=%d)", first, agent)
	}
	if agent > last {
		t.Errorf("agent inserted after last </body> at %d (agent=%d)", last, agent)
	}
}

func TestProxyModifyResponse_OversizedBodyPassesThrough(t *testing.T) {
	// Oversized text/html bodies are passed through untouched and the
	// X-Crit-Agent-Injection: skipped-oversized header is set so the chrome
	// can warn.
	huge := bytes.Repeat([]byte("A"), maxHTMLBodyBytes+1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(huge)
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Crit-Agent-Injection"); got != "skipped-oversized" {
		t.Errorf("X-Crit-Agent-Injection = %q, want skipped-oversized", got)
	}
}

func TestProxyModifyResponse_SkipsInjectionWhenNoBodyTag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><!-- no body tag -->")
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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

func TestProxyModifyResponse_CrossOriginRedirectStubEscaping(t *testing.T) {
	cases := []struct {
		name        string
		location    string
		mustNotHave []string // raw substrings that would indicate a break-out
		mustHave    []string // substrings that prove proper escaping
	}{
		{
			name:        "script_tag_breakout",
			location:    "https://evil.test/</script><script>alert(1)</script>",
			mustNotHave: []string{"</script><script>alert(1)"},
			mustHave:    []string{`</script>`, "&lt;/script&gt;"},
		},
		{
			name:     "u2028_line_separator",
			location: "https://evil.test/\u2028alert(1)",
			// json.Marshal escapes raw U+2028 (which terminates JS strings in
			// pre-ES2019 parsers) to the 6-char sequence \u2028.
			mustHave: []string{`url:"https://evil.test/\u2028alert(1)"`},
		},
		{
			name:     "double_quote_in_js_context",
			location: `https://evil.test/"+alert(1)+"`,
			// The JSON-marshalled URL must escape the embedded quote.
			mustHave: []string{`\"+alert(1)+\"`},
		},
		{
			name:        "html_link_injection",
			location:    `https://evil.test/"><img src=x onerror=alert(1)>`,
			mustNotHave: []string{`"><img src=x onerror=alert(1)>`},
			mustHave:    []string{"&lt;img src=x onerror=alert(1)&gt;", "&#34;"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(http.StatusFound)
			}))
			defer upstream.Close()
			proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
			s := string(body)
			for _, bad := range tc.mustNotHave {
				if strings.Contains(s, bad) {
					t.Errorf("body contains forbidden substring %q\nbody: %s", bad, s)
				}
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(s, want) {
					t.Errorf("body missing expected substring %q\nbody: %s", want, s)
				}
			}
		})
	}
}

func TestProxyModifyResponse_StripsCookieDomain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Set-Cookie", "foo=bar; Domain=upstream.test; Path=/; HttpOnly")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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

func TestProxyModifyResponse_RewritesCookieAttributes(t *testing.T) {
	tests := []struct {
		name      string
		setCookie string
		wantOK    func(string) bool
		desc      string
	}{
		{
			"strips Secure",
			"foo=bar; Secure; Path=/",
			func(sc string) bool {
				return !strings.Contains(strings.ToLower(sc), "secure") &&
					strings.Contains(sc, "foo=bar")
			},
			"Secure attribute should be stripped (proxy serves http on loopback)",
		},
		{
			"downgrades SameSite=None to Lax",
			"sess=abc; SameSite=None; Path=/",
			func(sc string) bool {
				lower := strings.ToLower(sc)
				return strings.Contains(lower, "samesite=lax") &&
					!strings.Contains(lower, "samesite=none") &&
					strings.Contains(sc, "sess=abc")
			},
			"SameSite=None must be downgraded to Lax (browsers reject None without Secure)",
		},
		{
			"strips Secure + downgrades SameSite=None together",
			"sess=abc; Secure; SameSite=None; Path=/",
			func(sc string) bool {
				lower := strings.ToLower(sc)
				return !strings.Contains(lower, "secure") &&
					strings.Contains(lower, "samesite=lax") &&
					!strings.Contains(lower, "samesite=none") &&
					strings.Contains(sc, "sess=abc")
			},
			"Combined https-staging cookie should land usable on http loopback",
		},
		{
			"preserves SameSite=Lax",
			"foo=bar; SameSite=Lax; Path=/",
			func(sc string) bool {
				return strings.Contains(strings.ToLower(sc), "samesite=lax")
			},
			"existing SameSite=Lax must not be removed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Set-Cookie", tt.setCookie)
				fmt.Fprintln(w, "<html><body>ok</body></html>")
			}))
			defer upstream.Close()
			proxy, _ := newLiveProxy(upstream.URL, 9001, "")
			ps := httptest.NewServer(proxy)
			defer ps.Close()
			resp, _ := http.Get(ps.URL + "/")
			if resp != nil {
				resp.Body.Close()
			}
			sc := resp.Header.Get("Set-Cookie")
			if !tt.wantOK(sc) {
				t.Errorf("%s — got Set-Cookie: %s", tt.desc, sc)
			}
		})
	}
}

// TestSWShim_StructureAndBalance is a cheap sanity check on the inline JS
// constant: balanced braces/parens/brackets, expected substrings present, and
// the <script> wrapper is closed exactly once. Catches accidental edits that
// would leave an unparseable shim — a real parser would be better but pulling
// in a JS engine for one constant isn't worth it.
func TestSWShim_StructureAndBalance(t *testing.T) {
	s := swShim
	if !strings.HasPrefix(s, "<script>") || !strings.HasSuffix(s, "</script>") {
		t.Fatalf("swShim not wrapped in <script> tags: %q", s)
	}
	if strings.Count(s, "<script>") != 1 || strings.Count(s, "</script>") != 1 {
		t.Errorf("expected exactly one <script>/</script> pair: %q", s)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(s, "<script>"), "</script>")
	pairs := map[rune]rune{')': '(', '}': '{', ']': '['}
	var stack []rune
	for _, c := range body {
		switch c {
		case '(', '{', '[':
			stack = append(stack, c)
		case ')', '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[c] {
				t.Fatalf("unbalanced bracket %q in shim", c)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		t.Fatalf("unclosed brackets in shim: %v", stack)
	}
	for _, want := range []string{
		"navigator.serviceWorker",
		"navigator.serviceWorker.register",
		"crit: service workers disabled",
		"getRegistrations",
		"unregister",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("shim missing %q", want)
		}
	}
}

func TestProxyModifyResponse_SWShimInjectedInHTMLHead(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><head><title>app</title></head><body><script>navigator.serviceWorker.register('/sw.js');</script></body></html>`)
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
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
	proxy, _ := newLiveProxy("http://127.0.0.1:19998", 9001, "")
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
	// The 502 envelope must not leak upstream URL/host details into the
	// browser response — log them to stderr instead.
	if strings.Contains(string(body), "127.0.0.1:19998") {
		t.Errorf("error body leaked upstream address: %s", body)
	}
	if strings.Contains(string(body), "detail") {
		t.Errorf("error body still contains detail field: %s", body)
	}
}

func TestBindProxyServer_PortIsAPIPlusOne(t *testing.T) {
	const maxAttempts = 20
	for attempt := 0; attempt < maxAttempts; attempt++ {
		apiLn, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		apiPort := apiLn.Addr().(*net.TCPAddr).Port

		ln, srv, err := bindProxyServer("http://127.0.0.1:19997", apiPort, "")
		if err != nil {
			apiLn.Close()
			if attempt < maxAttempts-1 && strings.Contains(err.Error(), "already in use") {
				continue
			}
			t.Fatalf("bindProxyServer: %v", err)
		}
		defer ln.Close()
		apiLn.Close()
		_ = srv
		if ln.Addr().(*net.TCPAddr).Port != apiPort+1 {
			t.Errorf("proxy port = %d, want %d", ln.Addr().(*net.TCPAddr).Port, apiPort+1)
		}
		return
	}
	t.Fatalf("could not bind proxy after %d attempts", maxAttempts)
}

func TestProxyModifyResponse_MidBodyReadFailureReturns502(t *testing.T) {
	// Upstream sends Content-Length but hijacks the connection and closes it
	// before writing any bytes. io.ReadAll returns ErrUnexpectedEOF with an
	// empty body — must surface as a 502 (matching upstream-failure UX),
	// not a blank 200 with agentInjected=false.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("hijacker not supported")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close()
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
	ps := httptest.NewServer(proxy)
	defer ps.Close()

	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 on mid-body upstream close", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"error"`) {
		t.Errorf("expected JSON error envelope, got: %s", body)
	}
}

func TestProxyModifyResponse_HeadInsideHTMLComment(t *testing.T) {
	// A literal <head> inside an HTML comment must not misroute the SW shim
	// — it must inject at the real <head>.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<!doctype html><!-- example: <head> --><html><head><title>x</title></head><body>hi</body></html>`)
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bs := string(body)
	commentEnd := strings.Index(bs, "-->")
	shim := strings.Index(bs, "crit: service workers disabled")
	title := strings.Index(bs, "<title>x</title>")
	if commentEnd < 0 || shim < 0 || title < 0 {
		t.Fatalf("missing markers: comment=%d shim=%d title=%d body=%s", commentEnd, shim, title, bs)
	}
	if shim < commentEnd {
		t.Errorf("shim injected inside/before HTML comment (shim=%d commentEnd=%d)", shim, commentEnd)
	}
	// Shim must land between the real <head> and its <title> child — i.e.,
	// inserted at the real <head>, not the commented one.
	if shim > title {
		t.Errorf("shim injected after <title>, not after the real <head> (shim=%d title=%d)", shim, title)
	}
}

func TestProxyModifyResponse_BodyCloseInsideHTMLComment(t *testing.T) {
	// </body> inside an HTML comment must not be picked up — agent bundle
	// belongs before the LAST real </body>.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><head></head><body><p>hi</p><!-- legacy </body> remnant --></body></html>`)
	}))
	defer upstream.Close()
	proxy, _ := newLiveProxy(upstream.URL, 9001, "")
	ps := httptest.NewServer(proxy)
	defer ps.Close()
	resp, err := http.Get(ps.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bs := string(body)
	agent := strings.Index(bs, "/crit-agent.js")
	commentClose := strings.Index(bs, "remnant -->")
	realBodyClose := strings.LastIndex(bs, "</body>")
	if agent < 0 || commentClose < 0 || realBodyClose < 0 {
		t.Fatalf("missing markers: agent=%d commentClose=%d realBodyClose=%d body=%s", agent, commentClose, realBodyClose, bs)
	}
	// Agent must inject after the comment closes (i.e., not before the
	// commented </body>) and before the real </body>.
	if agent < commentClose {
		t.Errorf("agent inserted before/inside HTML comment (agent=%d commentEnd=%d)", agent, commentClose)
	}
	if agent > realBodyClose {
		t.Errorf("agent inserted after real </body> (agent=%d realBody=%d)", agent, realBodyClose)
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

			handler, err := newLiveProxy(upstream.URL, 4101, "")
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
