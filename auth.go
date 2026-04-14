package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/term"
)

// runAuth dispatches to auth subcommands: login, logout, whoami.
func runAuth(args []string) {
	if len(args) == 0 {
		printAuthUsage()
		return
	}
	switch args[0] {
	case "login":
		runAuthLogin(args[1:])
	case "logout":
		runAuthLogout(args[1:])
	case "whoami":
		runAuthWhoami(args[1:])
	default:
		printAuthUsage()
	}
}

func printAuthUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit auth <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  login     Log in to crit-web")
	fmt.Fprintln(os.Stderr, "  logout    Log out and revoke token")
	fmt.Fprintln(os.Stderr, "  whoami    Show current user info")
	os.Exit(1)
}

// authLoginFlags holds parsed flags for crit auth login.
type authLoginFlags struct {
	force bool
}

func parseAuthLoginFlags(args []string) authLoginFlags {
	var f authLoginFlags
	for _, arg := range args {
		if arg == "--force" {
			f.force = true
		}
	}
	return f
}

func runAuthLogin(args []string) {
	flags := parseAuthLoginFlags(args)
	cfg := loadShareConfig()
	serverURL := resolveShareURL("", cfg, defaultShareURL)
	existingToken := resolveAuthToken(cfg)

	if existingToken != "" && !flags.force {
		if !confirmReauth() {
			return
		}
	}

	code, err := requestDeviceCode(serverURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n  Opening browser to sign in...\n")
	fmt.Fprintf(os.Stderr, "  If it doesn't open, visit: %s\n\n", code.VerificationURIComplete)
	go openBrowser(code.VerificationURIComplete)

	token, err := pollForToken(serverURL, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := saveAuthToken(token.AccessToken); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving token: %v\n", err)
		os.Exit(1)
	}

	// Cache user identity for the settings panel
	if token.UserName != "" || token.UserEmail != "" {
		_ = saveGlobalConfig(func(m map[string]json.RawMessage) error {
			if token.UserName != "" {
				name, _ := json.Marshal(token.UserName)
				m["auth_user_name"] = name
			}
			if token.UserEmail != "" {
				email, _ := json.Marshal(token.UserEmail)
				m["auth_user_email"] = email
			}
			return nil
		})
	}

	greeting := "Logged in."
	if token.UserName != "" {
		greeting = fmt.Sprintf("Logged in as %s.", token.UserName)
	}
	fmt.Fprintf(os.Stderr, "  %s Token saved to %s\n", greeting, globalConfigPath())
}

// confirmReauth prompts the user to confirm re-authentication.
// Returns true if the user confirms, false otherwise.
func confirmReauth() bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "Already logged in. Use --force to re-authenticate.")
		return false
	}
	fmt.Fprint(os.Stderr, "Already logged in. Log in again? (y/n) ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// deviceCodeResponse holds the response from POST /api/device/code.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

// requestDeviceCode initiates the device flow by requesting a device code.
func requestDeviceCode(serverURL string) (deviceCodeResponse, error) {
	var result deviceCodeResponse
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(serverURL+"/api/device/code", "application/json", nil)
	if err != nil {
		return result, fmt.Errorf("contacting %s: %w", serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return result, fmt.Errorf("%s", errBody.Error)
		}
		return result, fmt.Errorf("login is not available on this server")
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// tokenResponse holds a successful response from POST /api/device/token.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	UserName    string `json:"user_name"`
	UserEmail   string `json:"user_email"`
}

// pollForToken polls the device token endpoint until success, expiry, or cancellation.
func pollForToken(serverURL string, code deviceCodeResponse) (tokenResponse, error) {
	var result tokenResponse
	interval := code.Interval
	if interval < 1 {
		interval = 5
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dots := 0
	for {
		select {
		case <-ctx.Done():
			clearSpinner(dots)
			return result, fmt.Errorf("interrupted")
		case <-time.After(time.Duration(interval) * time.Second):
		}

		dots = printSpinner(dots)

		resp, err := pollDeviceToken(serverURL, code.DeviceCode)
		if err != nil {
			clearSpinner(dots)
			return result, err
		}

		if resp.done {
			clearSpinner(dots)
			return resp.token, nil
		}

		interval = resp.nextInterval(interval)
	}
}

// pollResult holds the parsed result of a single poll attempt.
type pollResult struct {
	done     bool
	token    tokenResponse
	slowDown bool
}

// nextInterval returns the interval to use for the next poll.
func (r pollResult) nextInterval(current int) int {
	if r.slowDown {
		next := current + 5
		if next > 60 {
			return 60
		}
		return next
	}
	return current
}

// pollDeviceToken makes a single poll request to the device token endpoint.
func pollDeviceToken(serverURL string, deviceCode string) (pollResult, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(serverURL+"/api/device/token", "application/json", bytes.NewReader(body))
	if err != nil {
		return pollResult{}, fmt.Errorf("contacting server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var token tokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
			return pollResult{}, fmt.Errorf("decoding token response: %w", err)
		}
		return pollResult{done: true, token: token}, nil
	}

	var errBody struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)

	return handlePollError(errBody.Error)
}

// handlePollError interprets the error string from a poll response.
func handlePollError(errStr string) (pollResult, error) {
	switch errStr {
	case "authorization_pending":
		return pollResult{}, nil
	case "slow_down":
		return pollResult{slowDown: true}, nil
	case "expired_token":
		return pollResult{}, fmt.Errorf("login timed out. Run 'crit auth login' to try again")
	default:
		return pollResult{}, fmt.Errorf("server error: %s", errStr)
	}
}

// printSpinner prints a dot and returns the updated count.
func printSpinner(dots int) int {
	fmt.Fprint(os.Stderr, ".")
	return dots + 1
}

// clearSpinner clears the spinner dots and moves to a new line.
func clearSpinner(dots int) {
	if dots > 0 {
		fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", dots+2)+"\r")
	}
}

// saveAuthToken writes the auth_token to the global config file.
func saveAuthToken(token string) error {
	return saveGlobalConfig(func(m map[string]json.RawMessage) error {
		raw, err := json.Marshal(token)
		if err != nil {
			return err
		}
		m["auth_token"] = raw
		return nil
	})
}

// removeAuthToken removes auth_token from the global config file.
func removeAuthToken() error {
	return saveGlobalConfig(func(m map[string]json.RawMessage) error {
		delete(m, "auth_token")
		return nil
	})
}

func runAuthLogout(args []string) {
	_ = args
	cfg := loadShareConfig()
	token := resolveAuthToken(cfg)

	if token == "" {
		fmt.Fprintln(os.Stderr, "  Not logged in.")
		return
	}

	if _, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		fmt.Fprintln(os.Stderr, "  Token is set via CRIT_AUTH_TOKEN environment variable and cannot be cleared by logout.")
		return
	}

	serverURL := resolveShareURL("", cfg, defaultShareURL)
	revoked := revokeToken(serverURL, token)

	if err := removeAuthToken(); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing token from config: %v\n", err)
		os.Exit(1)
	}

	// Clear cached identity
	_ = saveGlobalConfig(func(m map[string]json.RawMessage) error {
		delete(m, "auth_user_name")
		delete(m, "auth_user_email")
		return nil
	})

	if revoked {
		fmt.Fprintln(os.Stderr, "  Logged out.")
	} else {
		fmt.Fprintln(os.Stderr, "  Logged out locally. Could not reach server to revoke token.")
	}
}

// revokeToken calls DELETE /api/auth/token to revoke the token server-side.
// Returns true if the server acknowledged the revocation (204 or 401).
func revokeToken(serverURL string, token string) bool {
	req, err := http.NewRequest(http.MethodDelete, serverURL+"/api/auth/token", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusUnauthorized
}

func runAuthWhoami(args []string) {
	_ = args
	cfg := loadShareConfig()
	token := resolveAuthToken(cfg)

	if token == "" {
		fmt.Fprintln(os.Stderr, "  Not logged in. Run 'crit auth login' to authenticate.")
		return
	}

	serverURL := resolveShareURL("", cfg, defaultShareURL)
	name, email, err := fetchWhoami(serverURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Token is invalid or revoked. Run 'crit auth login' to re-authenticate.\n")
		return
	}

	if email != "" {
		fmt.Fprintf(os.Stderr, "  Logged in as %s (%s)\n", name, email)
	} else {
		fmt.Fprintf(os.Stderr, "  Logged in as %s\n", name)
	}
}

// whoamiResponse holds the response from GET /api/auth/whoami.
type whoamiResponse struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// fetchWhoami calls the whoami endpoint and returns the user's name and email.
func fetchWhoami(serverURL string, token string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/auth/whoami", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("contacting server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", fmt.Errorf("invalid or revoked token")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var result whoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decoding response: %w", err)
	}
	return result.Name, result.Email, nil
}

// errHintAlreadyShown is a sentinel error used by showLoginHint to skip
// the write when the hint was already shown.
var errHintAlreadyShown = errors.New("login hint already shown")

// showLoginHint prints a one-time hint about crit auth login after anonymous shares.
// Uses saveGlobalConfig for both read and write to avoid TOCTOU races.
func showLoginHint() {
	err := saveGlobalConfig(func(m map[string]json.RawMessage) error {
		if v, ok := m["login_hint_shown"]; ok && string(v) == "true" {
			return errHintAlreadyShown
		}
		fmt.Fprintln(os.Stderr, "  Tip: Run 'crit auth login' to link reviews to your account.")
		m["login_hint_shown"] = json.RawMessage("true")
		return nil
	})
	_ = err // best-effort hint; non-hint errors are also harmless
}
