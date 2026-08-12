package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	RuntimeMachineNeedsUpdate RuntimeState = "machine_needs_update"
	RuntimePermissionDenied   RuntimeState = "permission_denied"
	RuntimeUnsupportedVersion RuntimeState = "unsupported_version"
	RuntimeBroken             RuntimeState = "broken"
)

// ProbeResult is a diagnostic view of a container runtime. Unlike
// Engine.IsAvailable, it preserves the reason a runtime cannot be used.
type ProbeResult struct {
	Name        string
	State       RuntimeState
	Path        string
	Version     string
	Detail      string
	Warning     string
	MachineName string
	// MachineRunning is setup-policy input for safe repair plans. It is kept
	// below the user-facing payload because callers only need the resulting
	// commands, not another machine-state API.
	MachineRunning bool
	// ConflictingMachineName identifies a different running Podman machine that
	// prevents the dedicated Omnideck machine from starting on macOS. It remains
	// setup-policy input rather than part of the public runtime JSON contract.
	ConflictingMachineName string
}

// Ready reports whether this runtime can be used immediately.
func (p ProbeResult) Ready() bool { return p.State == RuntimeReady }

// probeLookPath and probeCommand are variables so the diagnostic behavior can
// be tested without requiring real container runtimes.
var probeLookPath = exec.LookPath

var probeCommand = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Env = hostCommandEnvironment(os.Environ())
	prepareHiddenConsoleCommand(command)
	return command.CombinedOutput()
}

// ProbeAll diagnoses Podman even when it is not yet usable.
func ProbeAll() []ProbeResult {
	return probeAllForOS(runtime.GOOS)
}

func prepareRuntimeCommand(name string) {
	refreshRuntimePath(name, runtime.GOOS)
}

func probeAllForOS(goos string) []ProbeResult {
	refreshRuntimePath("podman", goos)
	return []ProbeResult{probeRuntimeOnCurrentPath("podman", goos)}
}

// ReadyEngines converts successful probes into Engine implementations without
// running the same runtime checks a second time.
func ReadyEngines(probes []ProbeResult) []Engine {
	engines := make([]Engine, 0, len(probes))
	for _, probe := range probes {
		if !probe.Ready() {
			continue
		}
		if probe.Name == "podman" {
			engines = append(engines, &PodmanEngine{})
		}
	}
	return engines
}

func probeRuntime(name, goos string) ProbeResult {
	refreshRuntimePath(name, goos)
	return probeRuntimeOnCurrentPath(name, goos)
}

func probeRuntimeOnCurrentPath(name, goos string) ProbeResult {
	result := ProbeResult{Name: name, State: RuntimeMissing}
	path, err := probeLookPath(name)
	if err != nil {
		return result
	}
	result.Path = path
	result.Version = probeVersion(name)

	infoArgs := []string{"info"}
	if name == "podman" {
		infoArgs = podmanCommandArgs(goos, infoArgs...)
	}
	out, infoErr := probeCommand(name, infoArgs...)
	if infoErr == nil {
		result.State = RuntimeReady
		if policy := podmanPolicy(goos); policy.UsesMachine {
			result.MachineName = OmnideckMachineName
			if machines, ok := listPodmanMachines(); ok {
				if machine, found := omnideckPodmanMachine(machines); found {
					result.MachineRunning = machine.Running
					if goos == "windows" && !machine.UserModeNetworking {
						result.State = RuntimeMachineNeedsUpdate
					}
				}
			}
		}
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

	if name == "podman" && podmanPolicy(goos).UsesMachine {
		if state, machineName, ok, running, conflictingMachineName := probePodmanMachine(goos); ok {
			result.State = state
			result.MachineName = machineName
			result.MachineRunning = running
			result.ConflictingMachineName = conflictingMachineName
			return result
		}
	}

	if containsAny(lower,
		"cannot connect", "connection refused", "daemon is not running",
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

// refreshRuntimePath makes software installed while Omnideck is already open
// visible to this process. Installers can update the PATH used by future
// terminals, but an existing process keeps the PATH it inherited when it
// started.
func refreshRuntimePath(name, goos string) {
	refreshRuntimePathFromCandidates(name, goos, runtimePathCandidates(name, goos))
}

func runtimePathCandidates(name, goos string) []string {
	var candidates []string
	switch goos {
	case "windows":
		if name == "podman" {
			if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
				candidates = append(candidates,
					filepath.Join(localAppData, "Programs", "Podman"),
				)
			}
			if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
				candidates = append(candidates,
					filepath.Join(programFiles, "Podman"),
				)
			}
		}
	case "darwin":
		if name == "podman" {
			// Podman's official macOS package installs here and adds this
			// directory to /etc/paths.d for future terminal sessions.
			candidates = append(candidates,
				"/opt/podman/bin",
				"/opt/homebrew/bin",
				"/usr/local/bin",
			)
		}
	}
	return candidates
}

func refreshRuntimePathFromCandidates(name, goos string, candidates []string) {
	pathValue := os.Getenv("PATH")
	pathEntries := filepath.SplitList(pathValue)
	binaryName := name
	if goos == "windows" {
		binaryName += ".exe"
	}
	for _, candidate := range candidates {
		binary := filepath.Join(candidate, binaryName)
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() || pathEntryExists(pathEntries, candidate) {
			continue
		}
		pathEntries = append(pathEntries, candidate)
	}
	_ = os.Setenv("PATH", strings.Join(pathEntries, string(os.PathListSeparator)))
}

func pathEntryExists(entries []string, target string) bool {
	for _, entry := range entries {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(target)) {
			return true
		}
	}
	return false
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

type podmanMachine struct {
	Name               string `json:"Name"`
	Running            bool   `json:"Running"`
	Default            bool   `json:"Default"`
	UserModeNetworking bool   `json:"UserModeNetworking"`
}

func listPodmanMachines() ([]podmanMachine, bool) {
	out, err := probeCommand("podman", "machine", "list", "--format", "json")
	if err != nil {
		return nil, false
	}
	var machines []podmanMachine
	if err := json.Unmarshal(out, &machines); err != nil {
		return nil, false
	}
	return machines, true
}

func omnideckPodmanMachine(machines []podmanMachine) (podmanMachine, bool) {
	for _, machine := range machines {
		if machine.Name == OmnideckMachineName {
			return machine, true
		}
	}
	return podmanMachine{}, false
}

func conflictingPodmanMachine(machines []podmanMachine, goos string) string {
	if goos != "darwin" {
		return ""
	}
	for _, machine := range machines {
		if machine.Running && machine.Name != OmnideckMachineName {
			return machine.Name
		}
	}
	return ""
}

func probePodmanMachine(goos string) (RuntimeState, string, bool, bool, string) {
	machines, ok := listPodmanMachines()
	if !ok {
		return "", "", false, false, ""
	}
	machine, found := omnideckPodmanMachine(machines)
	if !found {
		return RuntimeMachineMissing, OmnideckMachineName, true, false, conflictingPodmanMachine(machines, goos)
	}
	if goos == "windows" && !machine.UserModeNetworking {
		return RuntimeMachineNeedsUpdate, OmnideckMachineName, true, machine.Running, ""
	}
	if machine.Running {
		// A named running machine with a failed info probe can be safely restarted;
		// setup targets only Omnideck's machine and preserves its data.
		return RuntimeBroken, OmnideckMachineName, true, true, ""
	}
	return RuntimeMachineStopped, OmnideckMachineName, true, false, conflictingPodmanMachine(machines, goos)
}

func applyVersionPolicy(result *ProbeResult, goos string) {
	_ = result
	_ = goos
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
	case RuntimeMachineNeedsUpdate:
		return "Installed; networking needs a one-time update"
	case RuntimePermissionDenied:
		return "Installed, but your account cannot use it"
	case RuntimeUnsupportedVersion:
		return "Installed, but too old for Omnideck"
	case RuntimeBroken:
		return "Installed, but needs attention"
	default:
		return "Installed, but needs attention"
	}
}
