package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestWindowsCgroupFailurePreservesCauseAndProvidesRecovery(t *testing.T) {
	cause := errors.New("exit status 127")
	detail := "Error: crun: open `memory.max` for writing: No such file or directory: OCI runtime attempted to invoke a command that was not found"
	err := runtimeCommandErrorForPlatform("windows", "podman run", cause, []byte(detail))
	var recovery *WSLCgroupError
	if !errors.As(err, &recovery) || !errors.Is(err, cause) {
		t.Fatalf("missing typed recovery or original cause: %v", err)
	}
	for _, want := range []string{detail, "exit status 127", "https://www.omnideck.dev/install.html#windows-wsl-recovery", "memory limit remains enabled"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestWindowsCgroupFailureDoesNotMisclassifyOtherErrors(t *testing.T) {
	for _, tc := range []struct {
		os, action, detail string
		want               bool
	}{
		{"windows", "podman start", "crun: controller `pids` is not available under /sys/fs/cgroup/wsl-user/distro-42/container/cgroup.controllers", true},
		{"linux", "podman run", "crun: open memory.max: No such file or directory", false},
		{"darwin", "podman run", "crun: open memory.max: No such file or directory", false},
		{"windows", "podman pull", "crun: open memory.max: No such file or directory", false},
		{"windows", "podman run", "open memory.max: No such file or directory", false},
		{"windows", "podman run", "crun: memory.max: Permission denied", false},
		{"windows", "podman run", "crun: controller pids is not available under /sys/fs/cgroup/user.slice", false},
		{"windows", "podman run", "crun: executable not found", false},
	} {
		if got := isWindowsWSLCgroupFailure(tc.os, tc.action, tc.detail); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}
