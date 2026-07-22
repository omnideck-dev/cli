package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openBrowserCmd launches the system browser for url.
func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		_ = openBrowser(url)
		return nil
	}
}

// copyToClipboard writes text to the system clipboard via pbcopy (macOS),
// clip (Windows), or xclip/xsel (Linux).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("clipboard tool not found — install xclip or xsel")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
