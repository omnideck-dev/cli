package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const windowsSetupRunOnceName = "OmnideckSetupResume"

var (
	runtimeExecutable = os.Executable
	runtimeRestartRun = func(name string, args ...string) error {
		command := exec.CommandContext(processCtx, name, args...)
		prepareHiddenConsoleCommand(command)
		return command.Run()
	}
)

// RestartAndResumeSetup registers the current CLI for a one-time relaunch,
// then asks Windows to restart. It is only called after the user chooses
// Restart now on the setup screen.
func RestartAndResumeSetup() error {
	if runtime.GOOS != "windows" {
		return errors.New("automatic restart is only available on Windows")
	}
	executable, err := runtimeExecutable()
	if err != nil {
		return fmt.Errorf("finding the omnideck executable: %w", err)
	}
	reg := windowsSystemTool("reg.exe")
	shutdown := windowsSystemTool("shutdown.exe")
	resumeCommand := windowsVisibleResumeCommand(windowsSystemTool("cmd.exe"), executable)
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\RunOnce`
	if err := runtimeRestartRun(
		reg, "add", key,
		"/v", windowsSetupRunOnceName,
		"/t", "REG_SZ", "/d", resumeCommand, "/f",
	); err != nil {
		return fmt.Errorf("scheduling setup to resume: %w", err)
	}
	if err := runtimeRestartRun(
		shutdown, "/r", "/t", "5",
		"/c", "omnideck setup will continue after you sign in.",
	); err != nil {
		_ = runtimeRestartRun(reg, "delete", key, "/v", windowsSetupRunOnceName, "/f")
		return fmt.Errorf("requesting the Windows restart: %w", err)
	}
	return nil
}

// windowsVisibleResumeCommand goes through cmd.exe's start builtin so Windows
// creates a new interactive console after sign-in. RunOnce itself does not
// guarantee inherited terminal handles, and launching the TUI directly can
// therefore consume the one-time entry and exit without showing a window.
func windowsVisibleResumeCommand(commandInterpreter, executable string) string {
	return strings.Join([]string{
		windowsQuoteCommand(commandInterpreter),
		"/d", "/c", "start", `"omnideck setup"`,
		windowsQuoteCommand(executable),
		"--resume-setup",
	}, " ")
}

func windowsSystemTool(name string) string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = os.Getenv("WINDIR")
	}
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", name)
}

func windowsQuoteCommand(executable string, args ...string) string {
	values := append([]string{executable}, args...)
	for i, value := range values {
		values[i] = `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return strings.Join(values, " ")
}
