//go:build darwin

package engine

import (
	"os/exec"
	"syscall"
)

// prepareHiddenConsoleCommand starts non-interactive helpers in a new session.
// A separate process group is not enough: the child still belongs to the TUI's
// terminal session, so Terminal.app can expose its name as the active process
// while the dashboard polls Podman. A new session has no controlling terminal
// and therefore cannot replace Omnideck in the terminal title.
func prepareHiddenConsoleCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func prepareVisibleCommand(command *exec.Cmd) {
	command.SysProcAttr = nil
}
