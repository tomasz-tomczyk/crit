package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// RunReviewClient connects to a running daemon, blocks until the user finishes
// reviewing, prints feedback to stdout, and returns whether the review was approved.
func RunReviewClient(entry SessionEntry, sessionKey string) (approved bool) {
	client := &http.Client{Timeout: 24 * time.Hour}

	statusCode, body, err := waitForDaemonReady(client, entry.Host, entry.Port, sessionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if statusCode == http.StatusInternalServerError {
		var status struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &status) == nil && status.Message != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", status.Message)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", body)
		}
		os.Exit(1)
	}

	resp, err := client.Post(entry.ConnURL()+"/api/review-cycle", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit daemon on port %d: %v\n", entry.Port, err)
		os.Exit(1)
	}

	body, err = readReviewCycleResponse(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	os.Stdout.Write(body)

	var result struct {
		Approved    bool   `json:"approved"`
		NextCommand string `json:"next_command"`
		Stats       *struct {
			Duration int `json:"duration_seconds"`
			Files    int `json:"files_reviewed"`
			Comments int `json:"comments_submitted"`
		} `json:"stats"`
	}
	if json.Unmarshal(body, &result) == nil {
		if !result.Approved && result.NextCommand != "" {
			fmt.Fprintf(os.Stdout, "\nNext round: %s\n", result.NextCommand)
		}
		if result.Approved && result.Stats != nil {
			printSessionSummary(result.Stats)
		}
		return result.Approved
	}
	return false
}

// RunReviewClientRaw is like RunReviewClient but returns (approved, prompt)
// without writing to stdout — used by plan hooks.
func RunReviewClientRaw(entry SessionEntry, sessionKey string) (approved bool, prompt string) {
	client := &http.Client{Timeout: 24 * time.Hour}

	if _, _, err := waitForDaemonReady(client, entry.Host, entry.Port, sessionKey); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: %v\n", err)
		return false, "crit daemon was unreachable; plan was not reviewed."
	}

	resp, err := client.Post(entry.ConnURL()+"/api/review-cycle", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not reach daemon: %v\n", err)
		return false, "crit daemon became unreachable before review was finished."
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not read daemon response: %v\n", err)
		return false, "crit daemon response could not be read."
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		fmt.Fprintf(os.Stderr, "crit plan-hook: daemon returned %d\n", resp.StatusCode)
		return false, "crit daemon returned an unexpected status."
	}

	var result struct {
		Approved bool   `json:"approved"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: malformed daemon response: %v\n", err)
		return false, "crit daemon returned a malformed response."
	}
	return result.Approved, result.Prompt
}

func waitForDaemonReady(client *http.Client, host string, port int, sessionKey string) (statusCode int, body []byte, err error) {
	connHost := host
	if connHost == "" {
		connHost = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%d", connHost, port)
	deadline := time.Now().Add(5 * time.Minute)
	for {
		resp, reqErr := client.Get(base + "/api/session")
		if reqErr != nil {
			if sessionKey != "" {
				if msg := ReadDaemonLog(sessionKey); msg != "" {
					return 0, nil, fmt.Errorf("%s", msg)
				}
			}
			return 0, nil, fmt.Errorf("could not reach daemon on port %d: %w", port, reqErr)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			return resp.StatusCode, respBody, nil
		}
		if time.Now().After(deadline) {
			return 0, nil, fmt.Errorf("daemon did not become ready within 5 minutes")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func readReviewCycleResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusGatewayTimeout {
		return nil, fmt.Errorf("timeout waiting for review")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return body, nil
}

func printSessionSummary(s *struct {
	Duration int `json:"duration_seconds"`
	Files    int `json:"files_reviewed"`
	Comments int `json:"comments_submitted"`
}) {
	if s.Files == 0 && s.Comments == 0 {
		return
	}
	var parts []string
	if s.Files > 0 {
		word := "files"
		if s.Files == 1 {
			word = "file"
		}
		parts = append(parts, fmt.Sprintf("%d %s", s.Files, word))
	}
	if s.Comments > 0 {
		word := "comments"
		if s.Comments == 1 {
			word = "comment"
		}
		parts = append(parts, fmt.Sprintf("%d %s", s.Comments, word))
	}
	if s.Duration > 0 {
		parts = append(parts, fmt.Sprintf("%ds", s.Duration))
	}
	fmt.Fprintf(os.Stderr, "\nSession: %s reviewed\n", joinParts(parts))
}

func joinParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return fmt.Sprintf("%s, and %s", joinParts(parts[:len(parts)-1]), parts[len(parts)-1])
	}
}
