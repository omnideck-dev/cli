package engine

import (
	"runtime"
	"strings"
	"testing"
)

func TestWindowsQuoteCommandPreservesPathsWithSpaces(t *testing.T) {
	got := windowsQuoteCommand(`C:\Program Files\Omnideck\omnideck.exe`, "--resume-setup")
	want := `"C:\Program Files\Omnideck\omnideck.exe" "--resume-setup"`
	if got != want {
		t.Fatalf("windowsQuoteCommand() = %q, want %q", got, want)
	}
}

func TestWindowsResumeCreatesAVisibleConsole(t *testing.T) {
	got := windowsVisibleResumeCommand(
		`C:\Windows\System32\cmd.exe`,
		`C:\Program Files\Omnideck\omnideck.exe`,
	)
	want := `"C:\Windows\System32\cmd.exe" /d /c start "omnideck setup" "C:\Program Files\Omnideck\omnideck.exe" --resume-setup`
	if got != want {
		t.Fatalf("windowsVisibleResumeCommand() = %q, want %q", got, want)
	}
}

func TestRestartAndResumeSetupRegistersBeforeRestart(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows RunOnce integration")
	}
	originalExecutable := runtimeExecutable
	originalRun := runtimeRestartRun
	t.Cleanup(func() {
		runtimeExecutable = originalExecutable
		runtimeRestartRun = originalRun
	})
	runtimeExecutable = func() (string, error) { return `C:\Omnideck\omnideck.exe`, nil }
	calls := []string{}
	runtimeRestartRun = func(name string, args ...string) error {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	if err := RestartAndResumeSetup(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(strings.ToLower(calls[0]), `\reg.exe add `) || !strings.Contains(strings.ToLower(calls[1]), `\shutdown.exe /r `) {
		t.Fatalf("calls = %#v, want RunOnce registration before restart", calls)
	}
	for _, want := range []string{`\cmd.exe`, `/d /c start "omnideck setup"`, `"C:\Omnideck\omnideck.exe"`, "--resume-setup"} {
		if !strings.Contains(strings.ToLower(calls[0]), strings.ToLower(want)) {
			t.Fatalf("RunOnce command %q is missing %q", calls[0], want)
		}
	}
}
