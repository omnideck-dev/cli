package engine

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// stubEngines sets up lookPath and runInfo stubs for the duration of a test.
// available is the set of engine names that should appear present and running.
func stubEngines(t *testing.T, available ...string) {
	t.Helper()
	set := make(map[string]bool, len(available))
	for _, n := range available {
		set[n] = true
	}
	origLP := lookPath
	origRI := runInfo
	lookPath = func(file string) (string, error) {
		if set[file] {
			return "/usr/bin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	runInfo = func(name string) error {
		if set[name] {
			return nil
		}
		return exec.ErrNotFound
	}
	t.Cleanup(func() {
		lookPath = origLP
		runInfo = origRI
	})
}

// TestDetectNone verifies that Detect returns an error when Podman is absent.
func TestDetectNone(t *testing.T) {
	stubEngines(t) // none available

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error when no engine available")
	}
}

func TestDetectPodman(t *testing.T) {
	stubEngines(t, "podman", "docker")

	eng, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng.Name() != "podman" {
		t.Errorf("expected podman, got %q", eng.Name())
	}
}

func TestDetectDoesNotFallBackToDocker(t *testing.T) {
	stubEngines(t, "docker")

	if eng, err := Detect(); err == nil || eng != nil {
		t.Fatalf("Detect() = %v, %v; want Podman-only failure", eng, err)
	}
}

func TestRuntimeCommandErrorKeepsEngineExplanation(t *testing.T) {
	err := runtimeCommandError(
		"podman run",
		errors.New("exit status 125"),
		[]byte("Error: address already in use\n"),
	)
	for _, want := range []string{"podman run", "exit status 125", "address already in use"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("runtime command error %q does not contain %q", err, want)
		}
	}
}

func TestCreateVolumeIfMissingReusesExistingVolume(t *testing.T) {
	createCalls := 0
	created, err := createVolumeIfMissing(
		"omnideck-home",
		func(string) (bool, error) { return true, nil },
		func() error {
			createCalls++
			return nil
		},
	)
	if err != nil || created || createCalls != 0 {
		t.Fatalf("createVolumeIfMissing() = %v, %t, create calls = %d; want reused volume", err, created, createCalls)
	}
}

func TestCreateVolumeIfMissingHandlesCreateRace(t *testing.T) {
	inspectCalls := 0
	created, err := createVolumeIfMissing(
		"omnideck-home",
		func(string) (bool, error) {
			inspectCalls++
			return inspectCalls > 1, nil
		},
		func() error { return errors.New("volume already exists") },
	)
	if err != nil || created {
		t.Fatalf("createVolumeIfMissing() = %v, %t, want race to reuse volume", err, created)
	}
}

func TestPullPodmanImageRetriesEmptyDockerCredentialHelperAnonymously(t *testing.T) {
	msgs := make(chan string, 1)
	calls := 0
	var retryAuthFile string
	err := pullPodmanImage(
		"ghcr.io/omnideck-dev/omnideck:latest",
		msgs,
		func(args ...string) error {
			calls++
			if calls == 1 {
				return errors.New(`podman pull: exit status 125
Error: error getting credentials - err: exec: "docker-credential-": executable file not found in $PATH`)
			}
			if len(args) != 4 || args[0] != "pull" || args[1] != "--authfile" ||
				args[3] != "ghcr.io/omnideck-dev/omnideck:latest" {
				t.Fatalf("retry arguments = %q; want pull --authfile <path> <image>", args)
			}
			retryAuthFile = args[2]
			data, readErr := os.ReadFile(args[2])
			if readErr != nil {
				t.Fatalf("reading retry auth file: %v", readErr)
			}
			if string(data) != "{}\n" {
				t.Fatalf("retry auth file = %q, want empty JSON object", data)
			}
			if info, statErr := os.Stat(args[2]); statErr != nil {
				t.Fatalf("stating retry auth file: %v", statErr)
			} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("retry auth file permissions = %o, want private", info.Mode().Perm())
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("pullPodmanImage() = %v, want successful retry", err)
	}
	if calls != 2 {
		t.Fatalf("pull calls = %d, want 2", calls)
	}
	if _, statErr := os.Stat(retryAuthFile); !os.IsNotExist(statErr) {
		t.Fatalf("retry auth file still exists after pull: %v", statErr)
	}
	if msg := <-msgs; !strings.Contains(msg, "Retrying") {
		t.Fatalf("retry message = %q, want plain-language retry explanation", msg)
	}
}

func TestPullPodmanImageDoesNotRetryOtherCredentialFailures(t *testing.T) {
	calls := 0
	wantErr := errors.New(`error getting credentials: exec: "docker-credential-desktop": executable file not found`)
	err := pullPodmanImage("example.invalid/image", nil, func(...string) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("pullPodmanImage() = %v, want original error", err)
	}
	if calls != 1 {
		t.Fatalf("pull calls = %d, want no retry", calls)
	}
}

// TestBuildPodmanRunArgsDoesNotReplace verifies setup cannot remove an
// unrelated container in a name-collision race.
func TestBuildPodmanRunArgsDoesNotReplace(t *testing.T) {
	opts := RunOptions{
		Name:        "omnideck",
		Image:       "ghcr.io/example/img:latest",
		ShmSize:     "256m",
		HomeVolume:  "omnideck-home",
		StateVolume: "omnideck-state",
		WebUIPort:   "2337",
		Platform:    "linux",
	}

	args := buildPodmanRunArgs(opts)
	assertNotContains(t, args, "--replace")
	assertContains(t, args, "--log-driver=k8s-file")
	assertContains(t, args, "--log-opt=max-size=150mb")
	assertContains(t, args, "127.0.0.1:2337:8080")
	assertContains(t, args, "ENABLE_DESKTOP=false")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.containers.internal:11434")
	assertNotContains(t, args, "--network")
	assertContains(t, args, "omnideck-home:/home/omnideck")
	assertContains(t, args, "omnideck-state:/var/lib/omnideck")
}

// TestBuildPodmanRunArgsMacOS verifies Podman macOS uses host.docker.internal.
func TestBuildPodmanRunArgsMacOS(t *testing.T) {
	opts := RunOptions{
		Name:        "omnideck",
		Image:       "ghcr.io/example/img:latest",
		ShmSize:     "256m",
		HomeVolume:  "omnideck-home",
		StateVolume: "omnideck-state",
		WebUIPort:   "2337",
		Platform:    "darwin",
	}

	args := buildPodmanRunArgs(opts)
	assertNotContains(t, args, "--replace")
	assertContains(t, args, "127.0.0.1:2337:8080")
	assertContains(t, args, "ENABLE_DESKTOP=false")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.docker.internal:11434")
	assertContains(t, args, "omnideck-home:/home/omnideck")
	assertContains(t, args, "omnideck-state:/var/lib/omnideck")
}

// TestBuildPodmanRunArgsWindows verifies the address used by both Omnideck and
// the post-setup connection check inside a Windows Podman machine.
func TestBuildPodmanRunArgsWindows(t *testing.T) {
	opts := RunOptions{
		Name:        "omnideck",
		Image:       "ghcr.io/example/img:latest",
		ShmSize:     "256m",
		HomeVolume:  "omnideck-home",
		StateVolume: "omnideck-state",
		WebUIPort:   "2337",
		Platform:    "windows",
	}

	args := buildPodmanRunArgs(opts, "192.168.127.254")
	assertContainsPrefix(t, args, "OLLAMA_HOST=http://host.containers.internal:11434")
	assertContains(t, args, "--add-host")
	assertContains(t, args, "host.containers.internal:192.168.127.254")
	if got := defaultOllamaURL("podman", "windows"); got != "http://host.containers.internal:11434" {
		t.Fatalf("Windows Podman Ollama URL = %q", got)
	}
}

func TestBuildPodmanRunArgsDoesNotAddWindowsHostOverrideOnOtherPlatforms(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		opts := RunOptions{
			Name:        "omnideck",
			Image:       "ghcr.io/example/img:latest",
			ShmSize:     "256m",
			HomeVolume:  "omnideck-home",
			StateVolume: "omnideck-state",
			Platform:    platform,
		}
		args := buildPodmanRunArgs(opts, "192.168.127.254")
		assertNotContains(t, args, "--add-host")
		assertNotContains(t, args, "host.containers.internal:192.168.127.254")
	}
}

func TestParseWindowsPodmanHostAddress(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"default via 192.168.127.1 dev podman-usermode\n", "192.168.127.1"},
		{"192.168.127.254 STREAM host.containers.internal\n", "192.168.127.254"},
		{"192.168.127.254 DGRAM\n192.168.127.254 RAW\n", "192.168.127.254"},
		{"999.168.127.254 STREAM invalid\n", ""},
		{"no address", ""},
	}
	for _, tt := range tests {
		if got := parseWindowsPodmanHostAddress(tt.output); got != tt.want {
			t.Errorf("parseWindowsPodmanHostAddress(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestUsesPodmanHostAlias(t *testing.T) {
	if !usesPodmanHostAlias("", "windows") {
		t.Fatal("default Windows Podman URL should use the Podman host alias")
	}
	if usesPodmanHostAlias("http://ollama.example:11434", "windows") {
		t.Fatal("custom Ollama URL should not request a Windows host override")
	}
}

func TestNormalizeOllamaURL(t *testing.T) {
	tests := []struct {
		value, runtimeName, platform, want string
	}{
		{"", "podman", "linux", "http://host.containers.internal:11434"},
		{"", "podman", "windows", "http://host.containers.internal:11434"},
		{"", "podman", "darwin", "http://host.docker.internal:11434"},
		{"localhost:11434", "podman", "windows", "http://localhost:11434"},
		{"https://ollama.example", "podman", "windows", "https://ollama.example"},
	}
	for _, tt := range tests {
		if got := normalizeOllamaURL(tt.value, tt.runtimeName, tt.platform); got != tt.want {
			t.Errorf("normalizeOllamaURL(%q, %q, %q) = %q, want %q", tt.value, tt.runtimeName, tt.platform, got, tt.want)
		}
	}
}

// TestBuildPodmanRunArgsMemorySet verifies --memory is included for Podman too.
func TestBuildPodmanRunArgsMemorySet(t *testing.T) {
	opts := RunOptions{
		Name:        "omnideck",
		Image:       "ghcr.io/example/img:latest",
		Memory:      "2g",
		ShmSize:     "1024m",
		HomeVolume:  "omnideck-home",
		StateVolume: "omnideck-state",
		Platform:    "linux",
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
