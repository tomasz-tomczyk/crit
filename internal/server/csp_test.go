package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func cspDirective(policy, name string) string {
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == name || strings.HasPrefix(d, name+" ") {
			return d
		}
	}
	return ""
}

func TestReviewPageShellsSendCSP(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.RemoteImages = []string{"img.shields.io", "*.githubusercontent.com"}

	for _, path := range []string{"/", "/index.html", "/live", "/preview", "/themes", "/themes.html"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "127.0.0.1:4567"
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			policy := w.Header().Get("Content-Security-Policy")
			if policy == "" {
				t.Fatal("missing Content-Security-Policy")
			}
			img := cspDirective(policy, "img-src")
			if img != "img-src 'self' data: blob: img.shields.io *.githubusercontent.com" {
				t.Errorf("img-src = %q", img)
			}
			if got := cspDirective(policy, "connect-src"); got != "connect-src 'self'" {
				t.Errorf("connect-src = %q", got)
			}
			if got := cspDirective(policy, "frame-src"); got != "frame-src 'self' http: https:" {
				t.Errorf("frame-src = %q, want live mode's frame to navigate anywhere", got)
			}
			if got := cspDirective(policy, "form-action"); got != "form-action 'self'" {
				t.Errorf("form-action = %q", got)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q", got)
			}
			if got := w.Header().Get("X-DNS-Prefetch-Control"); got != "off" {
				t.Errorf("X-DNS-Prefetch-Control = %q", got)
			}
			// Every inline script (any <script> without src, whatever its
			// attributes) must be allowed by hash, or the shell breaks.
			body := w.Body.Bytes()
			for _, m := range regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`).FindAllSubmatch(body, -1) {
				if strings.Contains(strings.ToLower(string(m[1])), "src") {
					continue
				}
				sum := sha256.Sum256(m[2])
				hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
				if !strings.Contains(cspDirective(policy, "script-src"), hash) {
					t.Errorf("inline script %.40q not allowed by script-src", m[2])
				}
			}
			if strings.Contains(cspDirective(policy, "script-src"), "'unsafe-inline'") {
				t.Error("script-src must not allow 'unsafe-inline'")
			}
		})
	}
}

func TestIndexHTMLCarriesRemoteImagesAllowlist(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.RemoteImages = []string{"img.shields.io"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	want := `<meta name="crit-remote-images" content="[&#34;img.shields.io&#34;]">`
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("index.html lacks %s", want)
	}
}

func TestReviewPageCSPRemoteImages(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantImg  string
	}{
		{"none", nil, "img-src 'self' data: blob:"},
		{"hosts", []string{"img.shields.io", "*.example.com"}, "img-src 'self' data: blob: img.shields.io *.example.com"},
		{"wildcard", []string{"img.shields.io", "*"}, "img-src 'self' data: blob: http: https:"},
		// Validated at config load; dropped again here because it is a header.
		{"injection", []string{"x.com; script-src *", "'unsafe-inline'"}, "img-src 'self' data: blob:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := reviewPageCSP(nil, tt.patterns)
			if got := cspDirective(policy, "img-src"); got != tt.wantImg {
				t.Errorf("img-src = %q, want %q", got, tt.wantImg)
			}
			if got := cspDirective(policy, "media-src"); got != strings.Replace(tt.wantImg, "img-src", "media-src", 1) {
				t.Errorf("media-src = %q", got)
			}
		})
	}
}

func TestInlineScriptHashes(t *testing.T) {
	page := []byte(`<script>a()</script><SCRIPT type="module">b()</SCRIPT><script src="x.js"></script><script defer src=y.js>ignored</script>`)
	got := inlineScriptHashes(page)
	want := []string{}
	for _, body := range []string{"a()", "b()"} {
		sum := sha256.Sum256([]byte(body))
		want = append(want, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("inlineScriptHashes = %v, want %v", got, want)
	}
}

func TestHandleRemoteImage(t *testing.T) {
	s, _ := newTestServer(t)
	tests := []struct {
		name     string
		method   string
		query    string
		wantCode int
	}{
		{"https", http.MethodGet, "src=https%3A%2F%2Fimg.example%2Fa.png&alt=A", http.StatusOK},
		{"http", http.MethodGet, "src=http%3A%2F%2Fimg.example%2Fa.png", http.StatusOK},
		{"javascript", http.MethodGet, "src=javascript%3Aalert(1)", http.StatusBadRequest},
		{"data", http.MethodGet, "src=data%3Aimage%2Fpng%3Bbase64%2CAAAA", http.StatusBadRequest},
		{"relative", http.MethodGet, "src=%2Fapi%2Fsession", http.StatusBadRequest},
		{"missing", http.MethodGet, "", http.StatusBadRequest},
		{"too long", http.MethodGet, "src=https%3A%2F%2Fa.example%2F" + strings.Repeat("a", maxRemoteImageURL), http.StatusBadRequest},
		{"post", http.MethodPost, "src=https%3A%2F%2Fimg.example%2Fa.png", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/remote-image?"+tt.query, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantCode)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			policy := w.Header().Get("Content-Security-Policy")
			for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "img-src http: https:", "frame-ancestors 'self'", "script-src " + remoteImageFrameScriptHash} {
				if !strings.Contains(policy, want) {
					t.Errorf("policy %q lacks %q", policy, want)
				}
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q", got)
			}
		})
	}
}

func TestHandleRemoteImageEscapes(t *testing.T) {
	s, _ := newTestServer(t)
	src := `https://img.example/a.png?x="><script>alert(1)</script>`
	alt := `"><script>alert(2)</script>`
	req := httptest.NewRequest(http.MethodGet, "/remote-image?src="+url.QueryEscape(src)+"&alt="+url.QueryEscape(alt), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	body, _ := io.ReadAll(w.Body)
	if n := strings.Count(string(body), "<script>"); n != 1 {
		t.Errorf("frame has %d <script> tags, want only its own:\n%s", n, body)
	}
}

func TestHandleRemoteImageTruncatesAltOnRuneBoundary(t *testing.T) {
	s, _ := newTestServer(t)
	alt := strings.Repeat("a", 1023) + "é"
	req := httptest.NewRequest(http.MethodGet, "/remote-image?src="+url.QueryEscape("https://img.example/a.png")+"&alt="+url.QueryEscape(alt), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `alt="`+strings.Repeat("a", 1023)+`"`) {
		t.Error("alt was not cut back to the last whole character")
	}
}
