//go:build !windows

package tui

import (
	"os"
	"os/exec"
	"runtime"
)

func openBrowser(url string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", url)
	} else if os.Getenv("WSL_DISTRO_NAME") != "" {
		cmd = exec.Command("cmd.exe", "/c", "start", "", url)
	} else {
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
