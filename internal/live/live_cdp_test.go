package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestNormalizeCDPBaseURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"9222", "http://9222"},
		{"127.0.0.1:9222", "http://127.0.0.1:9222"},
		{"http://127.0.0.1:9222/", "http://127.0.0.1:9222"},
		{"http://127.0.0.1:9222", "http://127.0.0.1:9222"},
	}
	for _, tc := range tests {
		got := normalizeCDPBaseURL(tc.in)
		if got != tc.want {
			t.Errorf("normalizeCDPBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveCDPURL(t *testing.T) {
	if got := resolveCDPURL("http://127.0.0.1:9333", "http://ignored"); got != "http://127.0.0.1:9333" {
		t.Fatalf("flag = %q", got)
	}
	if got := resolveCDPURL("", "127.0.0.1:9222"); got != "http://127.0.0.1:9222" {
		t.Fatalf("config = %q", got)
	}
	if got := resolveCDPURL("", ""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestFormatCDPCookies(t *testing.T) {
	got := formatCDPCookies([]cdpCookie{
		{Name: "session", Value: "abc"},
		{Name: "", Value: "skip"},
		{Name: "other", Value: "def"},
	})
	if got != "session=abc; other=def" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeCookieHeaders_LaterOverrides(t *testing.T) {
	got := mergeCookieHeaders("session=from_cdp; theme=dark", "session=manual")
	if got != "session=manual; theme=dark" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeCookieHeaders_EmptyParts(t *testing.T) {
	got := mergeCookieHeaders("", "a=1", "", "b=2")
	if got != "a=1; b=2" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchCDPCookies_FakeServer(t *testing.T) {
	var upgrader websocket.Upgrader
	mux := http.NewServeMux()

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		scheme := "ws"
		if r.TLS != nil {
			scheme = "wss"
		}
		host := r.Host
		_ = json.NewEncoder(w).Encode([]map[string]string{{
			"type":                 "page",
			"url":                  "about:blank",
			"webSocketDebuggerUrl": scheme + "://" + host + "/cdp",
		}})
	})

	mux.HandleFunc("/cdp", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var cmd cdpCommand
			if err := json.Unmarshal(msg, &cmd); err != nil {
				continue
			}
			switch cmd.Method {
			case "Network.enable":
				_ = conn.WriteJSON(map[string]any{"id": cmd.ID, "result": map[string]any{}})
			case "Network.getCookies":
				_ = conn.WriteJSON(cdpResponse{
					ID: cmd.ID,
					Result: cdpGetCookiesResult{
						Cookies: []cdpCookie{
							{Name: "session", Value: "from_browser", Domain: "127.0.0.1"},
						},
					},
				})
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := defaultFetchCDPCookies(context.Background(), srv.URL, srv.URL+"/app")
	if err != nil {
		t.Fatalf("fetchCDPCookies: %v", err)
	}
	if got != "session=from_browser" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchCDPCookies_Unreachable(t *testing.T) {
	_, err := defaultFetchCDPCookies(context.Background(), "http://127.0.0.1:1", "http://127.0.0.1:1/")
	if err == nil {
		t.Fatal("expected error for unreachable CDP endpoint")
	}
	if !strings.Contains(err.Error(), "listing Chrome targets") && !strings.Contains(err.Error(), "connecting to Chrome DevTools") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveLiveCookies_WithCDP(t *testing.T) {
	orig := fetchCDPCookies
	t.Cleanup(func() { fetchCDPCookies = orig })
	fetchCDPCookies = func(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
		if cdpBaseURL != "http://127.0.0.1:9222" {
			t.Fatalf("cdpBaseURL = %q", cdpBaseURL)
		}
		if originURL != "http://localhost:3000" {
			t.Fatalf("originURL = %q", originURL)
		}
		return "session=from_browser", nil
	}

	got, err := resolveLiveCookiesWithCDP(
		context.Background(),
		[]string{"session=manual"},
		"",
		"http://127.0.0.1:9222",
		Config{},
		"",
		"http://localhost:3000",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "session=manual" {
		t.Fatalf("got %q, want manual cookie to win", got)
	}
}

func TestResolveLiveCookies_WithCDPOnly(t *testing.T) {
	orig := fetchCDPCookies
	t.Cleanup(func() { fetchCDPCookies = orig })
	fetchCDPCookies = func(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
		return "session=from_browser", nil
	}

	got, err := resolveLiveCookiesWithCDP(context.Background(), nil, "", "http://127.0.0.1:9222", Config{}, "", "http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session=from_browser" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveLiveCookies_CDPErrors(t *testing.T) {
	orig := fetchCDPCookies
	t.Cleanup(func() { fetchCDPCookies = orig })
	fetchCDPCookies = func(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
		return "", context.DeadlineExceeded
	}

	_, err := resolveLiveCookiesWithCDP(context.Background(), nil, "", "http://127.0.0.1:9222", Config{}, "", "http://localhost:3000")
	if err == nil {
		t.Fatal("expected CDP error")
	}
}

func TestResolveLiveCookies_ConfigCDPURL(t *testing.T) {
	orig := fetchCDPCookies
	t.Cleanup(func() { fetchCDPCookies = orig })
	fetchCDPCookies = func(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
		if cdpBaseURL != "http://127.0.0.1:9333" {
			t.Fatalf("cdpBaseURL = %q", cdpBaseURL)
		}
		return "session=cfg", nil
	}
	got, err := resolveLiveCookiesWithCDP(context.Background(), nil, "", "", Config{LiveCDPURL: "127.0.0.1:9333"}, "", "http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session=cfg" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveLiveCookies_CDPFailsWithManualCookies(t *testing.T) {
	orig := fetchCDPCookies
	t.Cleanup(func() { fetchCDPCookies = orig })
	fetchCDPCookies = func(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
		return "", context.DeadlineExceeded
	}
	got, err := resolveLiveCookiesWithCDP(context.Background(), []string{"session=manual"}, "", "http://127.0.0.1:9222", Config{}, "", "http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session=manual" {
		t.Fatalf("got %q", got)
	}
}
