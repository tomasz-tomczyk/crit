package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type cdpCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}

type cdpGetCookiesResult struct {
	Cookies []cdpCookie `json:"cookies"`
}

type cdpResponse struct {
	ID     int                 `json:"id"`
	Result cdpGetCookiesResult `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type cdpCommand struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

var cdpHTTPClient = &http.Client{Timeout: 5 * time.Second}

// resolveCDPURL picks the CDP endpoint from the CLI flag first, then the config
// value, returning an empty string when neither is set.
func resolveCDPURL(flagCDPURL string, cfgLiveCDPURL string) string {
	if flagCDPURL != "" {
		return normalizeCDPBaseURL(flagCDPURL)
	}
	if cfgLiveCDPURL != "" {
		return normalizeCDPBaseURL(cfgLiveCDPURL)
	}
	return ""
}

func normalizeCDPBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}

func formatCDPCookies(cookies []cdpCookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+c.Value)
	}
	return joinCookieHeader(parts)
}

// mergeCookieHeaders merges cookie header fragments; later fragments override
// earlier cookies with the same name (explicit --cookie wins over CDP).
func mergeCookieHeaders(parts ...string) string {
	byName := make(map[string]string)
	order := make([]string, 0)
	for _, part := range parts {
		for _, pair := range splitCookiePairs(part) {
			name, _, ok := strings.Cut(pair, "=")
			if !ok || strings.TrimSpace(name) == "" {
				continue
			}
			if _, seen := byName[name]; !seen {
				order = append(order, name)
			}
			byName[name] = pair
		}
	}
	merged := make([]string, 0, len(order))
	for _, name := range order {
		merged = append(merged, byName[name])
	}
	return joinCookieHeader(merged)
}

func splitCookiePairs(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var fetchCDPCookies = defaultFetchCDPCookies

func defaultFetchCDPCookies(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
	if cdpBaseURL == "" {
		return "", nil
	}
	wsURL, err := cdpPageWebSocketURL(ctx, cdpBaseURL, originURL)
	if err != nil {
		return "", err
	}
	cookies, err := cdpNetworkGetCookies(ctx, wsURL, originURL)
	if err != nil {
		return "", err
	}
	return formatCDPCookies(cookies), nil
}

type cdpTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func cdpPageWebSocketURL(ctx context.Context, cdpBaseURL, originURL string) (string, error) {
	targets, err := cdpListTargets(ctx, cdpBaseURL)
	if err != nil {
		return "", err
	}
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}
	return cdpNewPageWebSocketURL(ctx, cdpBaseURL, originURL)
}

func cdpListTargets(ctx context.Context, cdpBaseURL string) ([]cdpTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdpBaseURL+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	resp, err := cdpHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing Chrome targets at %s: %w", cdpBaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("listing Chrome targets returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var targets []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decoding Chrome targets: %w", err)
	}
	return targets, nil
}

func cdpNewPageWebSocketURL(ctx context.Context, cdpBaseURL, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cdpBaseURL+"/json/new?"+url.QueryEscape(pageURL), nil)
	if err != nil {
		return "", err
	}
	resp, err := cdpHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("creating Chrome page target: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("creating Chrome page target returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var target cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return "", fmt.Errorf("decoding Chrome page target: %w", err)
	}
	if target.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("chrome page target did not return webSocketDebuggerUrl")
	}
	return target.WebSocketDebuggerURL, nil
}

func cdpNetworkGetCookies(ctx context.Context, wsURL, originURL string) ([]cdpCookie, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("opening Chrome DevTools WebSocket: %w", err)
	}
	defer conn.Close()

	if _, err := url.Parse(originURL); err != nil {
		return nil, fmt.Errorf("invalid origin URL %q: %w", originURL, err)
	}

	if err := cdpSend(conn, ctx, 1, "Network.enable", nil); err != nil {
		return nil, err
	}
	return cdpReadGetCookies(conn, ctx, 2, originURL)
}

func cdpReadGetCookies(conn *websocket.Conn, ctx context.Context, id int, originURL string) ([]cdpCookie, error) {
	cmd, err := json.Marshal(cdpCommand{
		ID:     id,
		Method: "Network.getCookies",
		Params: map[string]any{"urls": []string{originURL}},
	})
	if err != nil {
		return nil, err
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, cmd); err != nil {
		return nil, fmt.Errorf("sending Network.getCookies: %w", err)
	}

	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("reading Chrome DevTools response: %w", err)
		}
		var resp cdpResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("Network.getCookies: %s", resp.Error.Message)
		}
		return resp.Result.Cookies, nil
	}
}

func cdpSend(conn *websocket.Conn, ctx context.Context, id int, method string, params map[string]any) error {
	cmd, err := json.Marshal(cdpCommand{ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, cmd); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return err
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var resp struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return nil
	}
}
