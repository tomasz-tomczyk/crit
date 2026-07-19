// Package notify sends desktop notifications when a review round is ready.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const defaultTitle = "Crit"

type commandSpec struct {
	name string
	args []string
}

// RoundReady sends a desktop notification that review round n is ready.
// reviewURL may be empty. Failures are silent — notification is best-effort.
func RoundReady(round int, reviewURL string) {
	Desktop(defaultTitle, roundReadyBody(round, reviewURL), reviewURL)
}

// Desktop attempts a platform desktop notification. Best-effort; never panics.
func Desktop(title, body, openURL string) {
	if title == "" {
		title = defaultTitle
	}
	specs := desktopCommandSpecs(runtime.GOOS, title, body, openURL, commandExists)
	_ = tryDesktop(specs, runCommand)
}

func roundReadyBody(round int, reviewURL string) string {
	body := fmt.Sprintf("Round %d is ready for review", round)
	if u := strings.TrimSpace(reviewURL); u != "" {
		body += "\n" + u
	}
	return body
}

func tryDesktop(specs []commandSpec, run func(commandSpec) error) bool {
	for _, spec := range specs {
		if err := run(spec); err == nil {
			return true
		}
	}
	return false
}

func runCommand(spec commandSpec) error {
	return exec.Command(spec.name, spec.args...).Run()
}

func desktopCommandSpecs(goos, title, body, openURL string, hasCommand func(string) bool) []commandSpec {
	switch goos {
	case "darwin":
		return []commandSpec{{name: "osascript", args: []string{"-e", darwinNotifyScript(title, body, openURL)}}}
	case "linux":
		var specs []commandSpec
		if hasCommand("notify-send") {
			specs = append(specs, commandSpec{
				name: "notify-send",
				args: []string{"--app-name=crit", title, body},
			})
		}
		if hasCommand("zenity") {
			specs = append(specs, commandSpec{
				name: "zenity",
				args: []string{"--notification", "--text=" + title + ": " + body},
			})
		}
		return specs
	case "windows":
		return []commandSpec{{
			name: "powershell",
			args: []string{"-NoProfile", "-NonInteractive", "-Command", windowsToastScript(title, body)},
		}}
	default:
		return nil
	}
}

func darwinNotifyScript(title, body, openURL string) string {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	notifyBody := body
	if u := strings.TrimSpace(openURL); u != "" && !strings.Contains(body, u) {
		notifyBody = body + "\n" + u
	}
	return fmt.Sprintf(`display notification "%s" with title "%s"`, esc(notifyBody), esc(title))
}

func windowsToastScript(title, body string) string {
	esc := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	return fmt.Sprintf(
		`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null; `+
			`$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); `+
			`$text = $template.GetElementsByTagName('text'); `+
			`$text.Item(0).AppendChild($template.CreateTextNode('%s')) > $null; `+
			`$text.Item(1).AppendChild($template.CreateTextNode('%s')) > $null; `+
			`$toast = [Windows.UI.Notifications.ToastNotification]::new($template); `+
			`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('crit').Show($toast)`,
		esc(title), esc(body),
	)
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
