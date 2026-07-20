package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RuntimeState describes what, if anything, must happen before a container
// runtime can be used by Omnideck.
type RuntimeState string

const (
	RuntimeReady              RuntimeState = "ready"
	RuntimeMissing            RuntimeState = "missing"
	RuntimeStopped            RuntimeState = "stopped"
	RuntimeMachineMissing     RuntimeState = "machine_missing"
	RuntimeMachineStopped     RuntimeState = "machine_stopped"
	RuntimePermissionDenied   RuntimeState = "permission_denied"
	RuntimeUnsupportedVersion RuntimeState = "unsupported_version"
	RuntimeBroken             RuntimeState = "broken"
)

// ProbeResult is a diagnostic view of a container runtime. Unlike
// Engine.IsAvailable, it preserves the reason a runtime cannot be used.
type ProbeResult struct {
	Name    string
	State   RuntimeState
	Path    string
	Version string
	Detail  string
	Warning string
}

// Ready reports whether this runtime can be used immediately.
func (p ProbeResult) Ready() bool { return p.State == RuntimeReady }

// probeLookPath and probeCommand are variables so the diagnostic behavior can
// be tested without requiring real container runtimes.
var probeLookPath = exec.LookPath

var probeCommand = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ProbeAll diagnoses Podman and Docker even when neither runtime is usable.
func ProbeAll() []ProbeResult {
	return probeAllForOS(runtime.GOOS)
}

func probeAllForOS(goos string) []ProbeResult {
	names := []string{"podman", "docker"}
	results := make([]ProbeResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = probeRuntime(name, goos)
		}()
	}
	wg.Wait()
	return results
}

// ReadyEngines converts successful probes into Engine implementations without
// running the same runtime checks a second time.
func ReadyEngines(probes []ProbeResult) []Engine {
	engines := make([]Engine, 0, len(probes))
	for _, probe := range probes {
		if !probe.Ready() {
			continue
		}
		switch probe.Name {
		case "podman":
			engines = append(engines, &PodmanEngine{})
		case "docker":
			engines = append(engines, &DockerEngine{})
		}
	}
	return engines
}

func probeRuntime(name, goos string) ProbeResult {
	result := ProbeResult{Name: name, State: RuntimeMissing}
	path, err := probeLookPath(name)
	if err != nil {
		return result
	}
	result.Path = path
	result.Version = probeVersion(name)

	out, infoErr := probeCommand(name, "info")
	if infoErr == nil {
		result.State = RuntimeReady
		applyVersionPolicy(&result, goos)
		return result
	}

	detail := strings.TrimSpace(string(out))
	result.Detail = detail
	lower := strings.ToLower(detail)
	if containsAny(lower, "permission denied", "access is denied", "got permission denied") {
		result.State = RuntimePermissionDenied
		return result
	}

	if name == "podman" && (goos == "darwin" || goos == "windows") {
		if state, ok := probePodmanMachine(); ok {
			result.State = state
			return result
		}
	}

	if containsAny(lower,
		"cannot connect", "connection refused", "daemon is not running",
		"is the docker daemon running", "is docker desktop running",
		"podman machine is not running") {
		result.State = RuntimeStopped
		return result
	}

	result.State = RuntimeBroken
	if result.Detail == "" {
		result.Detail = infoErr.Error()
	}
	return result
}

func probeVersion(name string) string {
	out, err := probeCommand(name, "--version")
	if err != nil {
		return ""
	}
	for _, field := range strings.Fields(string(out)) {
		candidate := strings.Trim(field, "v,()")
		if _, _, _, ok := parseVersion(candidate); ok {
			return candidate
		}
	}
	return ""
}

func probePodmanMachine() (RuntimeState, bool) {
	out, err := probeCommand("podman", "machine", "list", "--format", "json")
	if err != nil {
		return "", false
	}
	var machines []struct {
		Running bool `json:"Running"`
	}
	if err := json.Unmarshal(out, &machines); err != nil {
		return "", false
	}
	if len(machines) == 0 {
		return RuntimeMachineMissing, true
	}
	for _, machine := range machines {
		if machine.Running {
			// A running machine with a failed `podman info` needs diagnostics rather
			// than another start attempt.
			return RuntimeBroken, true
		}
	}
	return RuntimeMachineStopped, true
}

func applyVersionPolicy(result *ProbeResult, goos string) {
	major, minor, _, ok := parseVersion(result.Version)
	if !ok {
		return
	}
	if result.Name == "docker" && goos == "linux" && versionLess(major, minor, 20, 10) {
		result.State = RuntimeUnsupportedVersion
		result.Detail = "Docker 20.10 or newer is required for secure host-service networking on Linux."
		return
	}
}

func parseVersion(value string) (int, int, int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.SplitN(value, ".", 4)
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	major, err := strconv.Atoi(numericPrefix(parts[0]))
	if err != nil {
		return 0, 0, 0, false
	}
	minor, err := strconv.Atoi(numericPrefix(parts[1]))
	if err != nil {
		return 0, 0, 0, false
	}
	patch := 0
	if len(parts) > 2 {
		if parsed, err := strconv.Atoi(numericPrefix(parts[2])); err == nil {
			patch = parsed
		}
	}
	return major, minor, patch, true
}

func numericPrefix(value string) string {
	var end int
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	return value[:end]
}

func versionLess(major, minor, wantMajor, wantMinor int) bool {
	return major < wantMajor || (major == wantMajor && minor < wantMinor)
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

// RuntimeStateLabel returns concise, user-facing diagnostic text.
func RuntimeStateLabel(state RuntimeState) string {
	switch state {
	case RuntimeReady:
		return "Ready to use"
	case RuntimeMissing:
		return "Not installed"
	case RuntimeStopped:
		return "Installed, but not running"
	case RuntimeMachineMissing:
		return "Installed; one-time setup is not finished"
	case RuntimeMachineStopped:
		return "Installed, but not running"
	case RuntimePermissionDenied:
		return "Installed, but your account cannot use it"
	case RuntimeUnsupportedVersion:
		return "Installed, but too old for Omnideck"
	default:
		return fmt.Sprintf("Installed, but needs attention (%s)", state)
	}
}
