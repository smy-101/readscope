// Package browser opens URLs in the user's system browser. WSL needs
// special care: xdg-open inside the distro cannot reach Windows apps, so
// wslview (from wslu) is preferred there when installed. READSCOPE_BROWSER
// overrides everything (same convention as BROWSER), which is also the
// seam tests and power users hook.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open launches the system browser at url. It blocks until the opener
// returns (openers return immediately; they don't wait for the page),
// so callers on a UI loop should run it off the main goroutine.
func Open(url string) error {
	cmd := Command(url)
	if cmd == nil {
		return fmt.Errorf("no browser command on %s", runtime.GOOS)
	}
	return cmd.Run()
}

// Command builds the exec.Cmd that would open url; nil means no command
// was found. Split out so tests can inspect the resolution without
// spawning anything.
func Command(url string) *exec.Cmd {
	if b := strings.Fields(os.Getenv("READSCOPE_BROWSER")); len(b) > 0 {
		args := append(b[1:], url)
		return exec.Command(b[0], args...)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		return exec.Command("open", url)
	default:
		if isWSL() {
			if _, err := exec.LookPath("wslview"); err == nil {
				return exec.Command("wslview", url)
			}
		}
		return exec.Command("xdg-open", url)
	}
}

// isWSL reports a Linux running under Windows (WSL1/WSL2): the kernel
// build string advertises Microsoft.
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(b)), "microsoft")
}
