package engine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/omnideck-dev/cli/cmd/debug"
)

// DockerEngine implements Engine using the docker CLI.
type DockerEngine struct{}

func (e *DockerEngine) Name() string { return "docker" }

func (e *DockerEngine) IsAvailable() bool {
	_, err := lookPath("docker")
	return err == nil
}

func (e *DockerEngine) HasPermission() bool {
	cmd := exec.Command("docker", "ps")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "permission denied") &&
			!strings.Contains(string(out), "Got permission denied")
	}
	return true
}

func (e *DockerEngine) ContainerExists(name string) (bool, error) {
	args := []string{"ps", "-a", "--filter", "name=^" + name + "$", "--format", "{{.Names}}"}
	cmd := buildCmd("docker", args...)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("docker ps: %w", err)
	}
	return strings.TrimSpace(string(out)) == name, nil
}

func (e *DockerEngine) PullImage(image string, msgs chan<- string) error {
	cmd := buildCmd("docker", "pull", image)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Discard raw engine output — it contains ANSI progress codes that corrupt
	// the Bubble Tea alt-screen. Callers receive structured messages via msgs.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker pull: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if msgs != nil {
			msgs <- scanner.Text()
		}
	}
	return cmd.Wait()
}

func (e *DockerEngine) RunContainer(opts RunOptions) error {
	args := buildRunArgs("docker", opts)
	cmd := buildCmd("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}
	return nil
}

func (e *DockerEngine) StopContainer(name string) error {
	cmd := buildCmd("docker", "stop", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	return nil
}

func (e *DockerEngine) StartContainer(name string) error {
	cmd := buildCmd("docker", "start", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker start: %w", err)
	}
	return nil
}

func (e *DockerEngine) RemoveContainer(name string) error {
	cmd := buildCmd("docker", "rm", "-f", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm: %w", err)
	}
	return nil
}

func (e *DockerEngine) ContainerStatus(name string) (string, error) {
	cmd := buildCmd("docker", "inspect", "--format", "{{.State.Status}}", name)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (e *DockerEngine) TailLogs(name string, follow bool, tail int) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail), name)
	cmd := buildCmd("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildRunArgs constructs the arguments for `docker run` from RunOptions.
func buildRunArgs(binary string, opts RunOptions) []string {
	restart := opts.Restart
	if restart == "" {
		restart = "always"
	}
	hostPort := opts.WebUIPort
	if hostPort == "" {
		hostPort = "2337"
	}

	args := []string{
		"run", "-d",
		"--name", opts.Name,
		"--restart", restart,
		"--shm-size=" + opts.ShmSize,
	}
	if opts.Memory != "" {
		args = append(args, "--memory="+opts.Memory)
	}

	// Map host port → container port 8080 (internal). This supports multi-instance (each
	// instance gets its own host port) and works regardless of whether the
	// container app reads the PORT env var.
	args = append(args, "-p", hostPort+":8080")

	// On Linux, add host-gateway so the container can reach host services
	// (e.g. Ollama at 127.0.0.1:11434) without --network host.
	if opts.Platform == "linux" {
		args = append(args, "--add-host=host-gateway:host-gateway")
	}

	// Run as the current host user on Linux so files written into volumes are
	// owned by the host user, preventing permission errors during uninstall.
	// Skipped on macOS — Docker Desktop's volume sharing handles ownership
	// differently and the UID space doesn't map the same way.
	if opts.Platform == "linux" {
		if u, err := user.Current(); err == nil {
			args = append(args, "--user", u.Uid+":"+u.Gid)
		}
	}

	// Volume mount — append :Z on Linux for SELinux.
	sharedMount := opts.SharedDir + ":/home/computron"
	stateMount := opts.StateDir + ":/var/lib/computron"
	if opts.Platform == "linux" {
		sharedMount += ":Z"
		stateMount += ":Z"
	}
	args = append(args, "-v", sharedMount, "-v", stateMount)

	// OLLAMA_HOST — always set so the container can find Ollama on the host.
	ollamaHost := opts.OllamaHost
	if ollamaHost == "" {
		if opts.Platform == "darwin" {
			ollamaHost = "host.docker.internal:11434"
		} else {
			ollamaHost = "host-gateway:11434"
		}
	}
	if !strings.HasPrefix(ollamaHost, "http") {
		ollamaHost = "http://" + ollamaHost
	}
	args = append(args, "-e", "OLLAMA_HOST="+ollamaHost)

	// PORT tells the container app which internal port to bind on.
	args = append(args, "-e", "PORT=8080")

	args = append(args, opts.Image)
	return args
}

// buildCmd builds an exec.Cmd and prints the command if debug mode is on.
func buildCmd(binary string, args ...string) *exec.Cmd {
	if debug.Enabled() {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", binary, strings.Join(args, " "))
	}
	return exec.Command(binary, args...)
}

// ContainerVersion returns the Docker version string.
func (e *DockerEngine) Version() string {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// ImageDigest returns the repo digest of the given image.
func (e *DockerEngine) ImageDigest(image string) string {
	cmd := exec.Command("docker", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Ensure DockerEngine implements Engine at compile time.
var _ Engine = (*DockerEngine)(nil)
