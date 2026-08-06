package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPodmanInstallerMatrixCoversMacAndWindowsArchitectures(t *testing.T) {
	tests := []struct {
		key      string
		version  string
		filename string
	}{
		{"windows-amd64", PodmanInstallerVersion, "podman-installer-windows-amd64.msi"},
		{"windows-arm64", PodmanInstallerVersion, "podman-installer-windows-arm64.msi"},
		{"darwin-arm64", PodmanInstallerVersion, "podman-installer-macos-arm64.pkg"},
		{"darwin-amd64", "v5.8.5", "podman-installer-macos-amd64.pkg"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			installer, ok := podmanInstallers[tt.key]
			if !ok || installer.Version != tt.version || installer.Filename != tt.filename || len(installer.SHA256) != 64 {
				t.Fatalf("installer = %#v, present=%t", installer, ok)
			}
		})
	}
}

func TestPrepareLinuxInstallCommandsChoosesSafeElevation(t *testing.T) {
	packageCommands := []SetupCommand{
		command("apt-get", "update"),
		command("apt-get", "install", "-y", "podman"),
	}
	lookupWith := func(names ...string) func(string) (string, error) {
		available := map[string]bool{}
		for _, name := range names {
			available[name] = true
		}
		return func(name string) (string, error) {
			if available[name] {
				return "/usr/bin/" + name, nil
			}
			return "", fmt.Errorf("%s not found", name)
		}
	}

	t.Run("root runs package manager directly", func(t *testing.T) {
		got, err := prepareLinuxInstallCommands(packageCommands, 0, false, lookupWith("apt-get"))
		if err != nil || got[0].Name != "apt-get" || got[0].Display != "apt-get update" {
			t.Fatalf("commands = %#v, error = %v", got, err)
		}
	})
	t.Run("desktop session prefers pkexec", func(t *testing.T) {
		got, err := prepareLinuxInstallCommands(packageCommands, 1000, false, lookupWith("apt-get", "pkexec", "sudo"))
		if err != nil || got[0].Name != "/usr/bin/pkexec" || strings.Join(got[0].Args, " ") != "apt-get update" || got[0].Display != "pkexec apt-get update" {
			t.Fatalf("commands = %#v, error = %v", got, err)
		}
	})
	t.Run("terminal session falls back to sudo", func(t *testing.T) {
		got, err := prepareLinuxInstallCommands(packageCommands, 1000, true, lookupWith("apt-get", "sudo"))
		if err != nil || got[1].Name != "/usr/bin/sudo" || strings.Join(got[1].Args, " ") != "apt-get install -y podman" {
			t.Fatalf("commands = %#v, error = %v", got, err)
		}
	})
	t.Run("desktop does not attempt a terminal-only sudo prompt", func(t *testing.T) {
		_, err := prepareLinuxInstallCommands(packageCommands, 1000, false, lookupWith("apt-get", "sudo"))
		if !errors.Is(err, errLinuxElevationMissing) {
			t.Fatalf("error = %v, want missing graphical elevation", err)
		}
	})
	t.Run("missing package manager is distinct from missing permission", func(t *testing.T) {
		_, err := prepareLinuxInstallCommands(packageCommands, 1000, true, lookupWith("sudo"))
		if !errors.Is(err, errLinuxPackageManagerMissing) {
			t.Fatalf("error = %v, want package-manager failure", err)
		}
	})
}

func TestEnsureRuntimeSkipsWorkWhenPodmanIsReady(t *testing.T) {
	events := []RuntimeSetupEvent{}
	hostCalls := 0
	result, err := EnsureRuntime(RuntimeSetupOptions{
		Host: HostPlatform{OS: "windows", Arch: "amd64"},
		probe: func() ProbeResult {
			return ProbeResult{Name: "podman", State: RuntimeReady, MachineName: OmnideckMachineName}
		},
		ensureHost: func(HostPlatform, func(RuntimeSetupEvent)) error {
			hostCalls++
			return nil
		},
		OnEvent: func(event RuntimeSetupEvent) { events = append(events, event) },
	})
	if err != nil || !result.Ready() {
		t.Fatalf("EnsureRuntime() = %#v, %v", result, err)
	}
	if hostCalls != 0 || len(events) != 0 {
		t.Fatalf("ready runtime performed setup: host calls=%d events=%#v", hostCalls, events)
	}
}

func TestEnsureRuntimeInstallsAndPreparesTheSharedMachine(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "podman", State: RuntimeMachineMissing},
		{Name: "podman", State: RuntimeReady, MachineName: OmnideckMachineName},
	}
	probeIndex := 0
	installCalls := 0
	commands := []SetupCommand{}
	events := []RuntimeSetupEvent{}
	result, err := EnsureRuntime(RuntimeSetupOptions{
		Host: HostPlatform{OS: "windows", Arch: "amd64"},
		probe: func() ProbeResult {
			result := probes[probeIndex]
			if probeIndex < len(probes)-1 {
				probeIndex++
			}
			return result
		},
		ensureHost: func(HostPlatform, func(RuntimeSetupEvent)) error { return nil },
		install: func(HostPlatform, string, func(RuntimeSetupEvent)) error {
			installCalls++
			return nil
		},
		runCommand: func(command SetupCommand, onOutput func(string)) error {
			commands = append(commands, command)
			onOutput("Preparing the secure workspace…")
			return nil
		},
		OnEvent: func(event RuntimeSetupEvent) { events = append(events, event) },
	})
	if err != nil || !result.Ready() {
		t.Fatalf("EnsureRuntime() = %#v, %v", result, err)
	}
	if installCalls != 1 || len(commands) != 1 {
		t.Fatalf("install calls=%d commands=%#v", installCalls, commands)
	}
	display := commands[0].Display
	for _, want := range []string{"machine init", "--provider wsl", "--user-mode-networking=true", OmnideckMachineName} {
		if !strings.Contains(display, want) {
			t.Fatalf("machine command %q is missing %q", display, want)
		}
	}
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Stage+":"+event.State] = true
	}
	for _, want := range []string{
		SetupStageSoftware + ":start",
		SetupStageSoftware + ":done",
		SetupStageEnvironment + ":start",
		SetupStageEnvironment + ":done",
	} {
		if !seen[want] {
			t.Fatalf("events %#v are missing %q", events, want)
		}
	}
}

func TestEnsureRuntimePreservesRestartGuidance(t *testing.T) {
	want := &RuntimeSetupError{
		Failure: RuntimeSetupRestart,
		Message: "Windows must restart.",
		Hint:    "Restart, then continue.",
	}
	_, err := EnsureRuntime(RuntimeSetupOptions{
		Host: HostPlatform{OS: "windows", Arch: "amd64"},
		probe: func() ProbeResult {
			return ProbeResult{Name: "podman", State: RuntimeMissing}
		},
		ensureHost: func(HostPlatform, func(RuntimeSetupEvent)) error { return want },
	})
	var got *RuntimeSetupError
	if !errors.As(err, &got) || got.Failure != RuntimeSetupRestart || got.Hint != want.Hint {
		t.Fatalf("error = %#v, want restart guidance", err)
	}
}

func TestDownloadVerifiedFileReusesAReviewedDownload(t *testing.T) {
	contents := []byte("reviewed podman installer")
	digest := sha256.Sum256(contents)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits++
		writer.Header().Set("Content-Length", "25")
		_, _ = writer.Write(contents)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "podman-installer")
	expected := hex.EncodeToString(digest[:])
	for attempt := 0; attempt < 2; attempt++ {
		if err := downloadVerifiedFile(server.URL, destination, expected, nil); err != nil {
			t.Fatalf("download attempt %d: %v", attempt+1, err)
		}
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want cached second attempt", hits)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(contents) {
		t.Fatalf("downloaded file = %q, %v", got, err)
	}
}
