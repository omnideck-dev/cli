//go:build darwin

package engine

import (
	"os/exec"
	"syscall"
)

// prepareHiddenConsoleCommand keeps non-interactive helpers out of the TUI's
// foreground process group. Terminal.app includes every process in that group
// in its title, so the dashboard's frequent Podman probes would otherwise make
// the title alternate between "podman — omnideck" and "omnideck".
func prepareHiddenConsoleCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func prepareVisibleCommand(command *exec.Cmd) {
	command.SysProcAttr = nil
}
