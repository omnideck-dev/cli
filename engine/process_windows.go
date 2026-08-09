//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// prepareHiddenConsoleCommand applies CREATE_NO_WINDOW to console helpers.
// This is deliberately different from SW_HIDE: SW_HIDE can affect the first
// window of a GUI process, while CREATE_NO_WINDOW only suppresses creation of
// a console for console-subsystem processes. UAC and other GUI prompts are
// therefore owned by a separate, visible process.
func prepareHiddenConsoleCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

func prepareVisibleCommand(command *exec.Cmd) {
	command.SysProcAttr = nil
}
