package engine

import (
	"errors"
	"os/exec"
	"runtime"
)

// RunOptions captures all parameters for running the Omnideck container.
type RunOptions struct {
	Name       string
	Image      string
	Memory     string // container memory limit (e.g. "2g")
	ShmSize    string
	SharedDir  string
	StateDir   string
	Network    string // "host"
	Restart    string // "always"
	OllamaHost string // 127.0.0.1 or host.docker.internal
	WebUIPort  string // host port for the web UI (e.g. "8080")
	Platform   string // runtime.GOOS
}

// Engine abstracts Docker/Podman operations.
type Engine interface {
	Name() string
	IsAvailable() bool
	HasPermission() bool
	ContainerExists(name string) (bool, error)
	PullImage(image string, msgs chan<- string) error
	RunContainer(opts RunOptions) error
	StopContainer(name string) error
	StartContainer(name string) error
	RemoveContainer(name string) error
	ContainerStatus(name string) (string, error)
	TailLogs(name string, follow bool, tail int) error
	Version() string
	ImageDigest(image string) string
}

// Detect tries Podman first, then Docker. Returns an error if neither is found.
func Detect() (Engine, error) {
	podman := &PodmanEngine{}
	if podman.IsAvailable() {
		return podman, nil
	}
	docker := &DockerEngine{}
	if docker.IsAvailable() {
		return docker, nil
	}
	return nil, errors.New("neither Docker nor Podman was found.\nInstall Docker: https://docs.docker.com/get-docker/")
}

// OllamaHost returns the OS-aware Ollama host:port string.
func OllamaHost() string {
	if runtime.GOOS == "darwin" {
		return "host.docker.internal:11434"
	}
	return "127.0.0.1:11434"
}

// lookPath is a variable so tests can override it.
var lookPath = exec.LookPath
