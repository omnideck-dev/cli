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

func TestWindowsMSIIsQuietAndWritesABoundedLog(t *testing.T) {
	args := windowsMSIArguments(`C:\cache\podman.msi`, `C:\cache\podman-install.log`)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/quiet") || strings.Contains(joined, "/passive") {
		t.Fatalf("MSI arguments = %q, want quiet installation", joined)
	}
	if !strings.Contains(joined, "/l*v C:\\cache\\podman-install.log") {
		t.Fatalf("MSI arguments = %q, want verbose log path", joined)
	}
}

func TestInstallerLogRetainsOnlyTheBoundedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installer.log")
	prefix := strings.Repeat("discarded\n", maxCommandOutput/5)
	tail := "Windows Installer returned error 1603\n"
	if err := os.WriteFile(path, []byte(prefix+tail), 0o600); err != nil {
		t.Fatal(err)
	}
	contents, err := retainBoundedLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) > maxCommandOutput || !strings.HasSuffix(string(contents), tail) {
		t.Fatalf("bounded log length=%d suffix=%q", len(contents), contents[max(0, len(contents)-len(tail)):])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxCommandOutput {
		t.Fatalf("retained installer log size=%d", info.Size())
	}
}

func TestWindowsWSLInstallScriptKeepsCatchAttachedToTry(t *testing.T) {
	script := windowsWSLInstallScript()
	if strings.Contains(script, "}; catch") {
		t.Fatal("PowerShell rejects a statement separator between try and catch")
	}
	if !strings.Contains(script, "} catch {") {
		t.Fatalf("expected an attached catch block, got %q", script)
	}
}

func TestWindowsWSLPostInstallStateRequiresRestartInsteadOfReportingFailure(t *testing.T) {
	err := windowsWSLPostInstallError("restart")
	var setupError *RuntimeSetupError
	if !errors.As(err, &setupError) || setupError.Failure != RuntimeSetupRestart {
		t.Fatalf("post-install restart error = %#v, want %q", err, RuntimeSetupRestart)
	}
	if err := windowsWSLPostInstallError("ready"); err != nil {
		t.Fatalf("ready post-install state returned %v", err)
	}
}

func TestMacOSAuthorizationCancellationIsDistinctFromInstallerFailure(t *testing.T) {
	cancelled := fmt.Errorf("osascript: exit status 1\nexecution error: %s (-128)", macOSAuthorizationCancelledMarker)
	if !macOSAuthorizationCancelled(cancelled) {
		t.Fatalf("error %q was not classified as a cancelled macOS authorization", cancelled)
	}
	if macOSAuthorizationCancelled(errors.New("installer: package scripts failed")) {
		t.Fatal("an installer failure was classified as a cancelled macOS authorization")
	}
}

func TestMacOSInstallerProcessBecomesVisibleOnlyAfterApproval(t *testing.T) {
	destination := "/Users/test/Library/Caches/omnideck-cli/downloads/podman.pkg"
	beforeApproval := `/usr/bin/osascript -e do shell script "/usr/sbin/installer -pkg " with administrator privileges ` + destination
	duringInstall := `/bin/sh -c /usr/sbin/installer -pkg '` + destination + `' -target /`
	if macOSInstallerProcessVisible(beforeApproval, destination) {
		t.Fatal("the authorization process was mistaken for a running installer")
	}
	if !macOSInstallerProcessVisible(duringInstall, destination) {
		t.Fatal("the approved installer process was not detected")
	}
	if macOSInstallerProcessVisible(`/usr/sbin/installer -pkg /tmp/other.pkg -target /`, destination) {
		t.Fatal("an unrelated installer process was treated as Podman setup")
	}
}

func TestSetupCommandFailureKeepsBoundedCommandOutput(t *testing.T) {
	environment := append(os.Environ(), "OMNIDECK_COMMAND_HELPER=1")
	err := runSetupCommand(
		os.Args[0],
		[]string{"-test.run=TestCommandHelperProcess"},
		environment,
		nil,
		nil,
		true,
	)
	if err == nil {
		t.Fatal("runSetupCommand() succeeded, want failure")
	}
	for _, want := range []string{"exit status 125", "Downloading layer", "machine is not ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("runSetupCommand() error %q does not contain %q", err, want)
		}
	}
}

func TestSetupSubstageProgressPreservesExactAndIndeterminateModes(t *testing.T) {
	exact := setupSubstageProgress(
		SetupStageSoftware,
		SetupSubstagePodmanDownload,
		"Downloading Podman…",
		"Downloading required software",
		"",
		0.47,
	)
	if exact.Substage != SetupSubstagePodmanDownload || exact.Progress == nil || *exact.Progress != 0.47 || exact.Status != "Downloading required software" {
		t.Fatalf("exact event = %#v", exact)
	}
	indeterminate := setupSubstageProgress(
		SetupStageSoftware,
		SetupSubstagePodmanInstall,
		"Installing Podman…",
		"Installer running",
		"",
		-1,
	)
	if indeterminate.Progress != nil || indeterminate.Substage != SetupSubstagePodmanInstall {
		t.Fatalf("indeterminate event = %#v", indeterminate)
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
	progressEvents := []verifiedDownloadProgress{}
	for attempt := 0; attempt < 2; attempt++ {
		if err := downloadVerifiedFile(server.URL, destination, expected, func(progress verifiedDownloadProgress) {
			progressEvents = append(progressEvents, progress)
		}); err != nil {
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
	last := progressEvents[len(progressEvents)-1]
	if last.Fraction != 1 || last.Received != int64(len(contents)) || last.Total != int64(len(contents)) {
		t.Fatalf("final download progress = %#v", last)
	}
	if got := formatDownloadBytes(38_400_000); got != "38.4 MB" {
		t.Fatalf("formatted download bytes = %q", got)
	}
}
