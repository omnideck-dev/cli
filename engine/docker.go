package engine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/omnideck-dev/cli/cmd/debug"
)

// DockerEngine implements Engine using the docker CLI.
type DockerEngine struct{}

func (e *DockerEngine) Name() string { return "docker" }

func (e *DockerEngine) IsAvailable() bool {
	if _, err := lookPath("docker"); err != nil {
		return false
	}
	// Verify the daemon is actually running, not just the binary present.
	return runInfo("docker") == nil
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

func (e *DockerEngine) CreateVolume(name string) error {
	cmd := buildCmd("docker", "volume", "create", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *DockerEngine) VolumeExists(name string) (bool, error) {
	cmd := buildCmd("docker", "volume", "inspect", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("docker volume inspect: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func (e *DockerEngine) RemoveVolume(name string) error {
	cmd := buildCmd("docker", "volume", "rm", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *DockerEngine) ExportVolume(name string, w io.Writer) error {
	cmd := buildCmd("docker", "run", "--rm", "-v", name+":/data", "alpine", "tar", "-C", "/data", "-c", ".")
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker volume export: %w", err)
	}
	return nil
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *DockerEngine) StartContainer(name string) error {
	cmd := buildCmd("docker", "start", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *DockerEngine) RemoveContainer(name string) error {
	cmd := buildCmd("docker", "rm", "-f", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w: %s", err, strings.TrimSpace(string(out)))
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

	args = append(args,
		"-v", opts.HomeVolume+":/home/omnideck",
		"-v", opts.StateVolume+":/var/lib/omnideck",
	)

	// OLLAMA_HOST — always set so the container can find Ollama on the host.
	ollamaHost := opts.OllamaHost
	if ollamaHost == "" {
		// Docker Desktop (macOS + Windows) resolves host.docker.internal to the
		// host automatically. On Linux there is no such name, so we point at
		// host-gateway, which is mapped via --add-host above.
		if opts.Platform == "linux" {
			ollamaHost = "host-gateway:11434"
		} else {
			ollamaHost = "host.docker.internal:11434"
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

// ContainerStats returns live CPU and memory stats for a running container.
// Calls `docker stats --no-stream` which blocks briefly; returns zero values for stopped containers.
func (e *DockerEngine) ContainerStats(name string) (cpu string, cpuPct float64, ram, ramTotal string, ramPct float64, err error) {
	cmd := exec.Command("docker", "stats", "--no-stream",
		"--format", "{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}", name)
	out, runErr := cmd.Output()
	if runErr != nil {
		return "", 0, "", "", 0, runErr
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", 0, "", "", 0, nil
	}
	parts := strings.SplitN(line, "\t", 3)
	cpu = strings.TrimSpace(parts[0])
	cpuPct = parsePctFloat(cpu)
	if len(parts) >= 2 {
		mem := strings.TrimSpace(parts[1])
		if slash := strings.Index(mem, "/"); slash >= 0 {
			ram = strings.TrimSpace(mem[:slash])
			ramTotal = strings.TrimSpace(mem[slash+1:])
		} else {
			ram = mem
		}
	}
	if len(parts) >= 3 {
		ramPct = parsePctFloat(strings.TrimSpace(parts[2]))
	}
	// Docker reports MemPerc as 0 when no memory limit is set (usage/∞ = 0).
	// Fall back to computing the ratio from the MemUsage "used / host_total" pair.
	if ramPct == 0 && ram != "" && ramTotal != "" {
		used := parseMemBytes(ram)
		total := parseMemBytes(ramTotal)
		if total > 0 {
			ramPct = used / total
		}
	}
	return cpu, cpuPct, ram, ramTotal, ramPct, nil
}

// ContainerInspect returns metadata about a container.
func (e *DockerEngine) ContainerInspect(name string) (InspectData, error) {
	format := `{{.State.StartedAt}}|{{.Created}}|{{.RestartCount}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}`
	cmd := buildCmd("docker", "inspect", "--format", format, name)
	out, err := cmd.Output()
	if err != nil {
		return InspectData{}, fmt.Errorf("docker inspect: %w", err)
	}
	return parseInspectLine(strings.TrimSpace(string(out)))
}

// FetchLogs returns the last tail lines of container log output (stdout + stderr).
func (e *DockerEngine) FetchLogs(name string, tail int) ([]string, error) {
	cmd := buildCmd("docker", "logs", "--tail", fmt.Sprintf("%d", tail), "--timestamps", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker logs: %w", err)
	}
	raw := strings.Split(strings.TrimSpace(string(out)), "\n")
	return raw, nil
}

// Ensure DockerEngine implements Engine at compile time.
var _ Engine = (*DockerEngine)(nil)
