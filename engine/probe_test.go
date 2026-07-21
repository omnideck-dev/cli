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

func TestRefreshRuntimePathFindsPerUserDocker(t *testing.T) {
	localAppData := t.TempDir()
	dockerBin := filepath.Join(localAppData, "Programs", "DockerDesktop", "resources", "bin")
	if err := os.MkdirAll(dockerBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dockerBin, "docker.exe"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("ProgramFiles", "")
	t.Setenv("PATH", "")

	refreshRuntimePath("docker", "windows")
	refreshRuntimePath("docker", "windows")

	entries := filepath.SplitList(os.Getenv("PATH"))
	count := 0
	for _, entry := range entries {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(dockerBin)) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("per-user Docker path appears %d times in PATH %q, want once", count, os.Getenv("PATH"))
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

func TestProbeDockerPermissionDenied(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "--version" {
			return []byte("Docker version 25.0.3, build abc"), nil
		}
		return []byte("permission denied while trying to connect to the Docker daemon socket"), errors.New("exit 1")
	})

	result := probeRuntime("docker", "linux")
	if result.State != RuntimePermissionDenied {
		t.Fatalf("state = %s, want %s", result.State, RuntimePermissionDenied)
	}
	if result.Version != "25.0.3" {
		t.Fatalf("version = %q, want 25.0.3", result.Version)
	}
}

func TestProbePodmanMachineMissing(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return []byte("podman version 5.2.1"), nil
		case "info":
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

func TestProbeOldDockerUnsupportedOnLinux(t *testing.T) {
	withProbeStubs(t, func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "--version" {
			return []byte("Docker version 19.03.15, build abc"), nil
		}
		return nil, nil
	})

	result := probeRuntime("docker", "linux")
	if result.State != RuntimeUnsupportedVersion {
		t.Fatalf("state = %s, want %s", result.State, RuntimeUnsupportedVersion)
	}
}

func TestParseVersion(t *testing.T) {
	major, minor, patch, ok := parseVersion("v5.3.2-rc1")
	if !ok || major != 5 || minor != 3 || patch != 2 {
		t.Fatalf("parseVersion = %d.%d.%d, %v", major, minor, patch, ok)
	}
}
