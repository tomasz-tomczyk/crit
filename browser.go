package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func openBrowser(url string) {
	time.Sleep(200 * time.Millisecond)
	if tryOpenBrowser(browserCommandSpecs(runtime.GOOS, url, systemIsWSL(), commandExists), runBrowserCommand) {
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: could not open browser automatically; open %s manually\n", url)
}

type browserCommandSpec struct {
	name string
	args []string
}

func tryOpenBrowser(specs []browserCommandSpec, run func(browserCommandSpec) error) bool {
	for _, spec := range specs {
		if err := run(spec); err == nil {
			return true
		}
	}
	return false
}

func runBrowserCommand(spec browserCommandSpec) error {
	return exec.Command(spec.name, spec.args...).Run()
}

func browserCommandSpecs(goos, url string, isWSL bool, hasCommand func(string) bool) []browserCommandSpec {
	switch goos {
	case "darwin":
		return []browserCommandSpec{{name: "open", args: []string{url}}}
	case "windows":
		// rundll32 url.dll is the most reliable way to open the default
		// browser on every supported Windows version. cmd /c start works too
		// but requires careful quoting around URLs containing & or %.
		return []browserCommandSpec{
			{name: "rundll32", args: []string{"url.dll,FileProtocolHandler", url}},
		}
	case "linux":
		var specs []browserCommandSpec
		if isWSL {
			if hasCommand("wslview") {
				specs = append(specs, browserCommandSpec{name: "wslview", args: []string{url}})
			}
			if hasCommand("powershell.exe") {
				specs = append(specs, browserCommandSpec{
					name: "powershell.exe",
					args: []string{
						"-NoProfile",
						"-NonInteractive",
						"-Command",
						"Start-Process " + powershellSingleQuote(url),
					},
				})
			}
			if hasCommand("cmd.exe") {
				specs = append(specs, browserCommandSpec{
					name: "cmd.exe",
					args: []string{"/c", `start "" ` + cmdDoubleQuote(url)},
				})
			}
		}
		if hasCommand("xdg-open") {
			specs = append(specs, browserCommandSpec{name: "xdg-open", args: []string{url}})
		}
		return specs
	default:
		return nil
	}
}

func powershellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func cmdDoubleQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func systemIsWSL() bool {
	versionData, err := os.ReadFile("/proc/version")
	if err != nil {
		versionData = nil
	}
	return looksLikeWSL(runtime.GOOS, os.Getenv("WSL_DISTRO_NAME"), os.Getenv("WSL_INTEROP"), string(versionData))
}

func looksLikeWSL(goos, distroName, interop, procVersion string) bool {
	if goos != "linux" {
		return false
	}
	if distroName != "" || interop != "" {
		return true
	}
	return strings.Contains(strings.ToLower(procVersion), "microsoft")
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
