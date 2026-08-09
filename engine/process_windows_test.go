//go:build windows

package engine

import (
	"os/exec"
	"testing"
)

func TestPrepareHiddenConsoleCommandUsesCreateNoWindow(t *testing.T) {
	command := exec.Command("wsl.exe", "--status")
	prepareHiddenConsoleCommand(command)
	if command.SysProcAttr == nil || command.SysProcAttr.CreationFlags != createNoWindow || command.SysProcAttr.HideWindow {
		t.Fatalf("Windows console helpers must use CREATE_NO_WINDOW, got %#v", command.SysProcAttr)
	}

	gui := exec.Command("msiexec.exe", "/quiet")
	prepareVisibleCommand(gui)
	if gui.SysProcAttr != nil {
		t.Fatalf("GUI installers must not receive console-hiding flags: %#v", gui.SysProcAttr)
	}
}
