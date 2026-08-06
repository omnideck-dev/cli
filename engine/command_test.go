package engine

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandOutputKeepsEngineStderr(t *testing.T) {
	cmd := commandHelper(t)
	_, err := commandOutput("podman ps", cmd)
	if err == nil {
		t.Fatal("commandOutput() succeeded, want failure")
	}
	for _, want := range []string{"podman ps", "exit status 125", "machine is not ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("commandOutput() error %q does not contain %q", err, want)
		}
	}
}

func TestStreamCommandOutputKeepsEngineStderr(t *testing.T) {
	msgs := make(chan string, 1)
	err := streamCommandOutput("podman pull", commandHelper(t), msgs)
	if err == nil {
		t.Fatal("streamCommandOutput() succeeded, want failure")
	}
	if got := <-msgs; got != "Downloading layer" {
		t.Fatalf("streamed message = %q, want download progress", got)
	}
	for _, want := range []string{"podman pull", "exit status 125", "machine is not ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("streamCommandOutput() error %q does not contain %q", err, want)
		}
	}
}

func TestRuntimeCommandErrorCleansTerminalFormatting(t *testing.T) {
	err := runtimeCommandError(
		"podman pull",
		fmt.Errorf("exit status 125"),
		[]byte("\x1b[31mError:\r\nmachine is not ready\x1b[0m\n"),
	)
	if strings.Contains(err.Error(), "\x1b") || strings.Contains(err.Error(), "\r") {
		t.Fatalf("runtime command error retained terminal formatting: %q", err)
	}
	if !strings.Contains(err.Error(), "machine is not ready") {
		t.Fatalf("runtime command error lost explanation: %q", err)
	}
}

func TestVolumeNotFoundOnlyMatchesMissingVolume(t *testing.T) {
	if !volumeNotFound("Error: no such volume omnideck-home") {
		t.Fatal("missing volume was not recognized")
	}
	if volumeNotFound("Error: cannot connect to Podman socket") {
		t.Fatal("connection failure must not be treated as a missing volume")
	}
}

func TestPodmanCommandsSelectTheSharedMachineOnlyOnDesktopVMPlatforms(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		got := podmanCommandArgs(goos, "container", "list")
		want := "--connection omnideck-runtime container list"
		if strings.Join(got, " ") != want {
			t.Fatalf("%s args = %q, want %q", goos, got, want)
		}
		machine := podmanCommandArgs(goos, "machine", "start", OmnideckMachineName)
		if strings.Join(machine, " ") != "machine start omnideck-runtime" {
			t.Fatalf("%s machine args = %q", goos, machine)
		}
	}
	if got := podmanCommandArgs("linux", "container", "list"); strings.Join(got, " ") != "container list" {
		t.Fatalf("linux args = %q", got)
	}
}

func commandHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestCommandHelperProcess")
	cmd.Env = append(os.Environ(), "OMNIDECK_COMMAND_HELPER=1")
	return cmd
}

func TestCommandHelperProcess(t *testing.T) {
	if os.Getenv("OMNIDECK_COMMAND_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "Downloading layer")
	fmt.Fprintln(os.Stderr, "Error: machine is not ready")
	os.Exit(125)
}
