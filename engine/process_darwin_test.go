//go:build darwin

package engine

import (
	"os/exec"
	"testing"
)

func TestPrepareHiddenConsoleCommandUsesSeparateSession(t *testing.T) {
	command := exec.Command("podman", "info")
	prepareHiddenConsoleCommand(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setsid {
		t.Fatalf("macOS background helpers must use a separate session, got %#v", command.SysProcAttr)
	}
	if command.SysProcAttr.Setpgid {
		t.Fatalf("macOS background helpers must not combine setsid and setpgid, got %#v", command.SysProcAttr)
	}

	prepareVisibleCommand(command)
	if command.SysProcAttr != nil {
		t.Fatalf("visible macOS helpers must remain in the terminal session, got %#v", command.SysProcAttr)
	}
}
