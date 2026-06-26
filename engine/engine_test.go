package engine

import (
	"os/exec"
	"testing"
)

// TestDetectNone verifies that Detect returns an error when neither docker nor
// podman is on PATH. We override lookPath to simulate a missing binary.
func TestDetectNone(t *testing.T) {
	orig := lookPath
	lookPath = func(file string) (string, error) {
		return "", exec.ErrNotFound
	}
	defer func() { lookPath = orig }()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error when no engine available")
	}
}

// TestDetectPodmanFirst verifies that Podman is preferred when both are available.
func TestDetectPodmanFirst(t *testing.T) {
	orig := lookPath
	lookPath = func(file string) (string, error) {
		// Both available.
		return "/usr/bin/" + file, nil
	}
	defer func() { lookPath = orig }()

	eng, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng.Name() != "podman" {
		t.Errorf("expected podman, got %q", eng.Name())
	}
}

// TestDetectDockerFallback verifies Docker is returned when Podman is absent.
func TestDetectDockerFallback(t *testing.T) {
	orig := lookPath
	lookPath = func(file string) (string, error) {
		if file == "podman" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/docker", nil
	}
	defer func() { lookPath = orig }()

	eng, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng.Name() != "docker" {
		t.Errorf("expected docker, got %q", eng.Name())
	}
}

// TestBuildRunArgsLinux verifies correct docker run args on Linux (with :Z mounts, port mapping, host-gateway).
func TestBuildRunArgsLinux(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/home/user/Omnideck",
		StateDir:  "/home/user/Omnideck/.state",
		Restart:   "always",
		WebUIPort: "2337",
		Platform:  "linux",
	}

	args := buildRunArgs("docker", opts)

	assertContains(t, args, "--shm-size=256m")
	assertContains(t, args, "2337:8080")
	assertContains(t, args, "--add-host=host-gateway:host-gateway")
	assertContains(t, args, "/home/user/Omnideck:/home/computron:Z")
	assertContains(t, args, "/home/user/Omnideck/.state:/var/lib/computron:Z")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host-gateway:11434")
	assertContains(t, args, "PORT=8080")
	assertNotContains(t, args, "--network")
	// --user should be present on Linux (value depends on test runner UID).
	assertContains(t, args, "--user")
}

// TestBuildRunArgsLinuxSecondInstance verifies a second instance maps a different host port.
func TestBuildRunArgsLinuxSecondInstance(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck2",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/home/user/Omnideck2",
		StateDir:  "/home/user/Omnideck2/.state",
		WebUIPort: "2338",
		Platform:  "linux",
	}

	args := buildRunArgs("docker", opts)

	assertContains(t, args, "2338:8080")
	// Internal PORT is always 8080 regardless of host port.
	assertContains(t, args, "PORT=8080")
}

// TestBuildRunArgsMacOS verifies macOS-specific behaviour (no :Z, no host-gateway, OLLAMA_HOST via docker.internal).
func TestBuildRunArgsMacOS(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/Users/user/Omnideck",
		StateDir:  "/Users/user/Omnideck/.state",
		WebUIPort: "2337",
		Platform:  "darwin",
	}

	args := buildRunArgs("docker", opts)

	assertContains(t, args, "2337:8080")
	assertNotContains(t, args, "/Users/user/Omnideck:/home/computron:Z")
	assertContains(t, args, "/Users/user/Omnideck:/home/computron")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.docker.internal:11434")
	assertNotContains(t, args, "--add-host=host-gateway:host-gateway")
	// --user must be absent on macOS — Docker Desktop handles ownership differently.
	assertNotContains(t, args, "--user")
}

// TestBuildRunArgsWindows verifies Windows uses host.docker.internal (Docker
// Desktop resolves it automatically) and omits Linux-only flags.
func TestBuildRunArgsWindows(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: `C:\Users\user\Omnideck`,
		StateDir:  `C:\Users\user\Omnideck\.state`,
		WebUIPort: "2337",
		Platform:  "windows",
	}

	args := buildRunArgs("docker", opts)

	assertContains(t, args, "2337:8080")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.docker.internal:11434")
	assertNotContains(t, args, "--add-host=host-gateway:host-gateway")
	// --user must be absent off Linux.
	assertNotContains(t, args, "--user")
}

// TestBuildPodmanRunArgsHasReplace verifies --replace is present and Linux uses host.containers.internal.
func TestBuildPodmanRunArgsHasReplace(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/home/user/Omnideck",
		StateDir:  "/home/user/Omnideck/.state",
		WebUIPort: "2337",
		Platform:  "linux",
	}

	args := buildPodmanRunArgs(opts)
	assertContains(t, args, "--replace")
	assertContains(t, args, "2337:8080")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.containers.internal:11434")
	assertNotContains(t, args, "--network")
	assertContains(t, args, "/home/user/Omnideck:/home/computron:Z,U")
	assertContains(t, args, "/home/user/Omnideck/.state:/var/lib/computron:Z,U")
}

// TestBuildPodmanRunArgsMacOS verifies Podman macOS uses host.docker.internal and no :Z,U mounts.
func TestBuildPodmanRunArgsMacOS(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/Users/user/Omnideck",
		StateDir:  "/Users/user/Omnideck/.state",
		WebUIPort: "2337",
		Platform:  "darwin",
	}

	args := buildPodmanRunArgs(opts)
	assertContains(t, args, "--replace")
	assertContains(t, args, "2337:8080")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.docker.internal:11434")
	// No SELinux or user-namespace flags on macOS.
	assertNotContains(t, args, "/Users/user/Omnideck:/home/computron:Z,U")
	assertNotContains(t, args, "/Users/user/Omnideck:/home/computron:Z")
	assertContains(t, args, "/Users/user/Omnideck:/home/computron")
}

// TestBuildRunArgsMemorySet verifies --memory is included when Memory is set.
func TestBuildRunArgsMemorySet(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		Memory:    "4g",
		ShmSize:   "2048m",
		SharedDir: "/home/user/Omnideck",
		StateDir:  "/home/user/Omnideck/.state",
		Platform:  "linux",
	}
	args := buildRunArgs("docker", opts)
	assertContains(t, args, "--memory=4g")
}

// TestBuildRunArgsMemoryEmpty verifies --memory is omitted when Memory is not set.
func TestBuildRunArgsMemoryEmpty(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		ShmSize:   "256m",
		SharedDir: "/home/user/Omnideck",
		StateDir:  "/home/user/Omnideck/.state",
		Platform:  "linux",
	}
	args := buildRunArgs("docker", opts)
	for _, a := range args {
		if len(a) >= 8 && a[:8] == "--memory" {
			t.Errorf("expected no --memory flag, got %q", a)
		}
	}
}

// TestBuildPodmanRunArgsMemorySet verifies --memory is included for Podman too.
func TestBuildPodmanRunArgsMemorySet(t *testing.T) {
	opts := RunOptions{
		Name:      "omnideck",
		Image:     "ghcr.io/example/img:latest",
		Memory:    "2g",
		ShmSize:   "1024m",
		SharedDir: "/home/user/Omnideck",
		StateDir:  "/home/user/Omnideck/.state",
		Platform:  "linux",
	}
	args := buildPodmanRunArgs(opts)
	assertContains(t, args, "--memory=2g")
}


// --- parsePctFloat ---

func TestParsePctFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0%", 0.0},
		{"100%", 1.0},
		{"50%", 0.5},
		{"  25.0%  ", 0.25},
		{"", 0.0},
		{"abc%", 0.0},
	}
	const eps = 1e-9
	for _, c := range cases {
		got := parsePctFloat(c.in)
		diff := got - c.want
		if diff < -eps || diff > eps {
			t.Errorf("parsePctFloat(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}

// --- parseMemBytes ---

func TestParseMemBytes(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"500MiB", 500 * (1 << 20)},
		{"1GiB", 1 << 30},
		{"2GB", 2e9},
		{"512KiB", 512 * (1 << 10)},
		{"1024B", 1024},
		{"0", 0},
	}
	for _, c := range cases {
		got := parseMemBytes(c.in)
		if got != c.want {
			t.Errorf("parseMemBytes(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMemBytesInvalid(t *testing.T) {
	got := parseMemBytes("abc")
	if got != 0 {
		t.Errorf("parseMemBytes(\"abc\"): got %v, want 0", got)
	}
}

// --- parseInspectLine ---

func TestParseInspectLineValid(t *testing.T) {
	line := "2024-01-15T12:00:00Z|2024-01-15T11:00:00Z|3|healthy"
	d, err := parseInspectLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.RestartCount != 3 {
		t.Errorf("RestartCount: got %d, want 3", d.RestartCount)
	}
	if d.HealthStatus != "healthy" {
		t.Errorf("HealthStatus: got %q, want 'healthy'", d.HealthStatus)
	}
	if d.StartedAt.IsZero() {
		t.Error("StartedAt should be parsed")
	}
}

func TestParseInspectLineHealthNone(t *testing.T) {
	line := "2024-01-15T12:00:00Z|2024-01-15T11:00:00Z|0|none"
	d, err := parseInspectLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.HealthStatus != "" {
		t.Errorf("HealthStatus 'none' should map to empty string, got %q", d.HealthStatus)
	}
}

func TestParseInspectLineNoValue(t *testing.T) {
	line := "2024-01-15T12:00:00Z|2024-01-15T11:00:00Z|0|<no value>"
	d, err := parseInspectLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.HealthStatus != "" {
		t.Errorf("HealthStatus '<no value>' should map to empty string, got %q", d.HealthStatus)
	}
}

func TestParseInspectLineTooFewFields(t *testing.T) {
	_, err := parseInspectLine("only|two|fields")
	if err == nil {
		t.Fatal("expected error for < 4 pipe-separated fields")
	}
}

// --- helpers ---

func assertContains(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("args %v: expected to contain %q", slice, want)
}

func assertNotContains(t *testing.T, slice []string, unwanted string) {
	t.Helper()
	for _, s := range slice {
		if s == unwanted {
			t.Errorf("args %v: should not contain %q", slice, unwanted)
			return
		}
	}
}

func assertContainsPrefix(t *testing.T, slice []string, prefix string) {
	t.Helper()
	for _, s := range slice {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return
		}
	}
	t.Errorf("args %v: expected an element with prefix %q", slice, prefix)
}

