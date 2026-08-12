package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// RunOptions captures all parameters for running the Omnideck container.
type RunOptions struct {
	Name        string
	Image       string
	Memory      string // container memory limit (e.g. "2g")
	ShmSize     string
	HomeVolume  string
	StateVolume string
	Restart     string // "always"
	OllamaHost  string // optional explicit host URL
	WebUIPort   string // host port for the web UI (e.g. "8080")
	Platform    string // runtime.GOOS
}

// InspectData holds container inspection metadata.
type InspectData struct {
	StartedAt    time.Time
	CreatedAt    time.Time
	RestartCount int
	HealthStatus string // "healthy", "unhealthy", "starting", "" = no healthcheck
}

// Engine abstracts the Podman operations Omnideck needs.
type Engine interface {
	Name() string
	IsAvailable() bool
	HasPermission() bool
	ContainerExists(name string) (bool, error)
	// CreateVolume ensures a named volume exists. created is true only when this
	// call created the volume, allowing transaction rollback to preserve volumes
	// that belonged to an earlier installation.
	CreateVolume(name string) (created bool, err error)
	VolumeExists(name string) (bool, error)
	RemoveVolume(name string) error
	ExportVolume(name string, w io.Writer) error
	PullImage(image string, msgs chan<- string) error
	RunContainer(opts RunOptions) error
	// CheckOllamaConnection tests the same container-to-host address Omnideck
	// receives. It runs from inside an existing Omnideck container.
	CheckOllamaConnection(name string) error
	StopContainer(name string) error
	StartContainer(name string) error
	RemoveContainer(name string) error
	ContainerStatus(name string) (string, error)
	// TailLogs writes container log output to stdout/stderr as it's produced,
	// keeping the two streams separate exactly as the container emitted them.
	// Interactive callers pass os.Stdout/os.Stderr; --follow --json passes the
	// same NDJSON-formatting writer for both, since a single JSON stream has
	// no separate-stream concept — the engine stays agnostic of output format
	// either way.
	TailLogs(name string, follow bool, tail int, stdout, stderr io.Writer) error
	Version() string
	ImageDigest(image string) string
	// ContainerStats returns live CPU and memory stats. cpu is e.g. "12.3%",
	// cpuPct is that as a [0,1] fraction, ram is e.g. "1.24 GiB", ramTotal is
	// the container memory limit e.g. "15.6 GiB", and ramPct is [0,1].
	ContainerStats(name string) (cpu string, cpuPct float64, ram, ramTotal string, ramPct float64, err error)
	// FetchLogs returns the last tail lines of container log output.
	FetchLogs(name string, tail int) ([]string, error)
	// ContainerInspect returns metadata about a container (started time, restarts, health).
	ContainerInspect(name string) (InspectData, error)
}

// Detect returns the Podman engine when it is ready. Omnideck deliberately
// uses one runtime on every platform so the desktop and CLI always see the
// same machines, containers, images, and volumes.
func Detect() (Engine, error) {
	all := DetectAll()
	if len(all) == 0 {
		return nil, errors.New("Podman is not ready.\nRun `omnideck` for guided setup")
	}
	return all[0], nil
}

// DetectAll returns Podman when it is ready.
func DetectAll() []Engine {
	podman := &PodmanEngine{}
	if podman.IsAvailable() {
		return []Engine{podman}
	}
	return nil
}

// ByName returns Podman, the only runtime supported by Omnideck.
func ByName(name string) (Engine, error) {
	switch name {
	case "podman":
		e := &PodmanEngine{}
		if !e.IsAvailable() {
			return nil, errors.New("podman not found")
		}
		return e, nil
	default:
		return nil, fmt.Errorf("unknown container runtime %q (Omnideck uses Podman)", name)
	}
}

// lookPath is a variable so tests can override it.
var lookPath = exec.LookPath

// runInfo executes "<name> info" to verify the daemon is running.
// It is a variable so tests can override it without spawning real processes.
var runInfo = func(name string) error {
	args := []string{"info"}
	if name == "podman" {
		args = podmanCommandArgs(runtime.GOOS, args...)
	}
	command := exec.Command(name, args...)
	command.Env = hostCommandEnvironment(os.Environ())
	prepareHiddenConsoleCommand(command)
	return command.Run()
}

// parsePctFloat strips a trailing "%" and returns the value as a [0,1] fraction.
func parsePctFloat(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	f, _ := strconv.ParseFloat(s, 64)
	return f / 100.0
}

// parseMemBytes parses memory strings like "500MiB", "1.24GiB", "2.1GB" to bytes.
// Podman reports memory as e.g. "500MiB / 15.5GiB" from {{.MemUsage}}.
func parseMemBytes(s string) float64 {
	s = strings.TrimSpace(s)
	for _, m := range []struct {
		suf string
		fac float64
	}{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1},
	} {
		if strings.HasSuffix(s, m.suf) {
			v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, m.suf)), 64)
			if err == nil {
				return v * m.fac
			}
		}
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// inspectTimeFormats lists the time layouts tried when parsing inspect timestamps.
// Podman versions can emit RFC3339Nano or a space-separated layout with a named TZ.
var inspectTimeFormats = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02T15:04:05.999999999 -0700 MST",
	time.RFC3339,
}

func parseInspectTime(s string) time.Time {
	for _, layout := range inspectTimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseInspectLine parses the pipe-delimited output produced by ContainerInspect format strings.
func parseInspectLine(s string) (InspectData, error) {
	parts := strings.SplitN(s, "|", 4)
	if len(parts) < 4 {
		return InspectData{}, fmt.Errorf("unexpected inspect output: %q", s)
	}
	var d InspectData
	d.StartedAt = parseInspectTime(parts[0])
	d.CreatedAt = parseInspectTime(parts[1])
	d.RestartCount, _ = strconv.Atoi(parts[2])
	d.HealthStatus = parts[3]
	if d.HealthStatus == "none" || d.HealthStatus == "<no value>" {
		d.HealthStatus = ""
	}
	return d, nil
}
