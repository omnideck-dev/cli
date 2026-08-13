//go:build darwin

package engine

import (
	"os/exec"
	"testing"
)

func TestPrepareHiddenConsoleCommandUsesSeparateProcessGroup(t *testing.T) {
	command := exec.Command("podman", "info")
	prepareHiddenConsoleCommand(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatalf("macOS background helpers must use a separate process group, got %#v", command.SysProcAttr)
	}

	prepareVisibleCommand(command)
	if command.SysProcAttr != nil {
		t.Fatalf("visible macOS helpers must remain in the foreground process group, got %#v", command.SysProcAttr)
	}
}
