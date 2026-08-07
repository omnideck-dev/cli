package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withProbeStubs(t *testing.T, command func(string, ...string) ([]byte, error)) {
	t.Helper()
	originalLookPath := probeLookPath
	originalCommand := probeCommand
	probeLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	probeCommand = command
	t.Cleanup(func() {
		probeLookPath = originalLookPath
		probeCommand = originalCommand
	})
}

func TestProbeRuntimeMissing(t *testing.T) {
	originalLookPath := probeLookPath
	probeLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { probeLookPath = originalLookPath })

	result := probeRuntime("podman", "linux")
	if result.State != RuntimeMissing {
		t.Fatalf("state = %s, want %s", result.State, RuntimeMissing)
	}
}

func TestRefreshRuntimePathFindsPerUserPodman(t *testing.T) {
	localAppData := t.TempDir()
	podmanBin := filepath.Join(localAppData, "Programs", "Podman")
	if err := os.MkdirAll(podmanBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(podmanBin, "podman.exe"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("ProgramFiles", "")
	t.Setenv("PATH", "")

	refreshRuntimePath("podman", "windows")
	refreshRuntimePath("podman", "windows")

	entries := filepath.SplitList(os.Getenv("PATH"))
	count := 0
	for _, entry := range entries {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(podmanBin)) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("per-user Podman path appears %d times in PATH %q, want once", count, os.Getenv("PATH"))
	}
}

func TestRefreshRuntimePathFindsNewMacOSPodmanInstall(t *testing.T) {
	podmanBin := filepath.Join(t.TempDir(), "podman", "bin")
	if err := os.MkdirAll(podmanBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(podmanBin, "podman"), []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")

	refreshRuntimePathFromCandidates("podman", "darwin", []string{podmanBin})
	refreshRuntimePathFromCandidates("podman", "darwin", []string{podmanBin})

	entries := filepath.SplitList(os.Getenv("PATH"))
	count := 0
	for _, entry := range entries {
		if filepath.Clean(entry) == filepath.Clean(podmanBin) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("macOS Podman path appears %d times in PATH %q, want once", count, os.Getenv("PATH"))
	}
}

func TestMacOSRuntimePathCandidatesIncludeOfficialPodmanInstaller(t *testing.T) {
	candidates := runtimePathCandidates("podman", "darwin")
	for _, candidate := range candidates {
		if candidate == "/opt/podman/bin" {
			return
		}
	}
	t.Fatalf("macOS Podman candidates = %q, want /opt/podman/bin", candidates)
}

func TestProbePodmanPermissionDenied(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "--version" {
			return []byte("podman version 6.0.2"), nil
		}
		return []byte("permission denied while connecting to Podman"), errors.New("exit 1")
	})

	result := probeRuntime("podman", "linux")
	if result.State != RuntimePermissionDenied {
		t.Fatalf("state = %s, want %s", result.State, RuntimePermissionDenied)
	}
	if result.Version != "6.0.2" {
		t.Fatalf("version = %q, want 6.0.2", result.Version)
	}
}

func TestProbePodmanMachineMissing(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 5.2.1"), nil
		case "--connection omnideck-runtime info":
			return []byte("cannot connect to Podman"), errors.New("exit 1")
		case "machine list --format json":
			return []byte("[]"), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	})

	result := probeRuntime("podman", "darwin")
	if result.State != RuntimeMachineMissing {
		t.Fatalf("state = %s, want %s", result.State, RuntimeMachineMissing)
	}
}

func TestProbePodmanReadyReportsTheSharedOmnideckMachine(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 6.0.2"), nil
		case "--connection omnideck-runtime info":
			return []byte("ready"), nil
		case "machine list --format json":
			return []byte(`[
				{"Name":"other","Running":true,"Default":false},
				{"Name":"developer-machine","Running":true,"Default":true}
			]`), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	})

	result := probeRuntime("podman", "windows")
	if result.State != RuntimeReady || result.MachineName != OmnideckMachineName {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbeWindowsMachineReportsRequiredNetworkingMigration(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 6.0.2"), nil
		case "--connection omnideck-runtime info":
			return []byte("ready"), nil
		case "machine list --format json":
			return []byte(`[{"Name":"omnideck-runtime","Running":true,"UserModeNetworking":false}]`), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	})

	result := probeRuntime("podman", "windows")
	if result.State != RuntimeMachineNeedsUpdate || !result.MachineRunning {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbeMacDoesNotApplyWindowsNetworkingMigration(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 6.0.2"), nil
		case "--connection omnideck-runtime info":
			return []byte("cannot connect to Podman"), errors.New("exit 1")
		case "machine list --format json":
			return []byte(`[{"Name":"omnideck-runtime","Running":false,"UserModeNetworking":false}]`), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	})

	result := probeRuntime("podman", "darwin")
	if result.State != RuntimeMachineStopped {
		t.Fatalf("state = %s, want %s", result.State, RuntimeMachineStopped)
	}
}

func TestProbeRunningSharedMachineWithBrokenConnectionCanBeRestarted(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
				switch strings.Join(args, " ") {
				case "--version":
					return []byte("podman version 6.0.2"), nil
				case "--connection omnideck-runtime info":
					return []byte("ssh connection failed"), errors.New("exit 1")
				case "machine list --format json":
					return []byte(`[{"Name":"omnideck-runtime","Running":true,"UserModeNetworking":true}]`), nil
				default:
					return nil, fmt.Errorf("unexpected command: %v", args)
				}
			})

			result := probeRuntime("podman", goos)
			if result.State != RuntimeBroken || !result.MachineRunning {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestProbeDoesNotAdoptAnUnrelatedPodmanMachine(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 6.0.2"), nil
		case "--connection omnideck-runtime info":
			return []byte("connection not found"), errors.New("exit 1")
		case "machine list --format json":
			return []byte(`[{"Name":"developer-machine","Running":true,"Default":true}]`), nil
		default:
			return nil, fmt.Errorf("unexpected command: %v", args)
		}
	})

	result := probeRuntime("podman", "windows")
	if result.State != RuntimeMachineMissing || result.MachineName != OmnideckMachineName {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbePodmanThreeRemainsUsable(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "--version" {
			return []byte("podman version 3.4.4"), nil
		}
		return nil, nil
	})

	result := probeRuntime("podman", "linux")
	if result.State != RuntimeReady {
		t.Fatalf("state = %s, want %s", result.State, RuntimeReady)
	}
	if result.Warning != "" {
		t.Fatalf("Podman must not be rejected or warned about based only on a major version: %q", result.Warning)
	}
}

func TestParseVersion(t *testing.T) {
	major, minor, patch, ok := parseVersion("v5.3.2-rc1")
	if !ok || major != 5 || minor != 3 || patch != 2 {
		t.Fatalf("parseVersion = %d.%d.%d, %v", major, minor, patch, ok)
	}
}
