//go:build integration

package live

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestLiveCDPIntegration_FetchCookiesFromChrome(t *testing.T) {
	chromeBin := findChromeBinary(t)
	if chromeBin == "" {
		t.Skip("Chrome/Chromium not found; set CHROME_BIN to run CDP integration tests")
	}

	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=integration-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer originSrv.Close()

	originURL := originSrv.URL
	port := freeTCPPort(t)
	profileDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, chromeBin,
		"--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		fmt.Sprintf("--user-data-dir=%s", profileDir),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting Chrome: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})

	cdpBase := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForCDP(t, ctx, cdpBase)

	if err := cdpSetCookie(ctx, cdpBase, originURL, "session", "integration-secret"); err != nil {
		t.Fatalf("cdpSetCookie: %v", err)
	}

	got, err := defaultFetchCDPCookies(ctx, cdpBase, originURL)
	if err != nil {
		t.Fatalf("defaultFetchCDPCookies: %v", err)
	}
	if got != "session=integration-secret" {
		t.Fatalf("got %q, want session=integration-secret", got)
	}

	merged, err := resolveLiveCookiesWithCDP(ctx, []string{"session=manual"}, "", cdpBase, Config{}, "", originURL)
	if err != nil {
		t.Fatalf("resolveLiveCookiesWithCDP: %v", err)
	}
	if merged != "session=manual" {
		t.Fatalf("merged = %q, want manual override", merged)
	}
}

func findChromeBinary(t *testing.T) string {
	t.Helper()
	if bin := strings.TrimSpace(os.Getenv("CHROME_BIN")); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	if runtime.GOOS == "darwin" {
		candidates = append([]string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}, candidates...)
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForCDP(t *testing.T, ctx context.Context, cdpBase string) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpBase+"/json/version", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Chrome DevTools at %s did not become ready", cdpBase)
}

func cdpSetCookie(ctx context.Context, cdpBaseURL, originURL, name, value string) error {
	wsURL, err := cdpNewPageWebSocketURL(ctx, cdpBaseURL, originURL)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := cdpSend(conn, ctx, 1, "Network.enable", nil); err != nil {
		return err
	}
	return cdpSend(conn, ctx, 2, "Network.setCookie", map[string]any{
		"name":  name,
		"value": value,
		"url":   originURL,
	})
}

func TestLiveCDPIntegration_SmokeUsesFetchedCookies(t *testing.T) {
	chromeBin := findChromeBinary(t)
	if chromeBin == "" {
		t.Skip("Chrome/Chromium not found; set CHROME_BIN to run CDP integration tests")
	}

	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=integration-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer originSrv.Close()
	originURL := originSrv.URL
	port := freeTCPPort(t)
	profileDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, chromeBin,
		"--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		fmt.Sprintf("--user-data-dir=%s", profileDir),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting Chrome: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})

	cdpBase := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForCDP(t, ctx, cdpBase)
	if err := cdpSetCookie(ctx, cdpBase, originURL, "session", "integration-secret"); err != nil {
		t.Fatalf("cdpSetCookie: %v", err)
	}

	cookies, err := resolveLiveCookiesWithCDP(ctx, nil, "", cdpBase, Config{}, "", originURL)
	if err != nil {
		t.Fatalf("resolveLiveCookiesWithCDP: %v", err)
	}
	result := runSmokeTest(originURL, cookies)
	if result.fatal {
		t.Fatalf("smoke failed: %+v", result)
	}
	if result.kind != smokeOK {
		t.Fatalf("kind = %v, want smokeOK", result.kind)
	}
}
