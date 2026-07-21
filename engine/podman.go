package engine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// PodmanEngine implements Engine using the podman CLI.
type PodmanEngine struct{}

func (e *PodmanEngine) Name() string { return "podman" }

func (e *PodmanEngine) IsAvailable() bool {
	prepareRuntimeCommand("podman")
	if _, err := lookPath("podman"); err != nil {
		return false
	}
	// Verify the daemon/machine is actually running, not just the binary present.
	return runInfo("podman") == nil
}

// HasPermission always returns true for rootless Podman.
func (e *PodmanEngine) HasPermission() bool { return true }

func (e *PodmanEngine) ContainerExists(name string) (bool, error) {
	args := []string{"ps", "-a", "--filter", "name=^" + name + "$", "--format", "{{.Names}}"}
	cmd := buildCmd("podman", args...)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("podman ps: %w", err)
	}
	return strings.TrimSpace(string(out)) == name, nil
}

func (e *PodmanEngine) CreateVolume(name string) error {
	cmd := buildCmd("podman", "volume", "create", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman volume create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *PodmanEngine) VolumeExists(name string) (bool, error) {
	cmd := buildCmd("podman", "volume", "inspect", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("podman volume inspect: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func (e *PodmanEngine) RemoveVolume(name string) error {
	cmd := buildCmd("podman", "volume", "rm", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman volume rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *PodmanEngine) ExportVolume(name string, w io.Writer) error {
	cmd := buildCmd("podman", "volume", "export", name)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman volume export: %w", err)
	}
	return nil
}

func (e *PodmanEngine) PullImage(image string, msgs chan<- string) error {
	cmd := buildCmd("podman", "pull", image)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("podman pull: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if msgs != nil {
			msgs <- scanner.Text()
		}
	}
	return cmd.Wait()
}

func (e *PodmanEngine) RunContainer(opts RunOptions) error {
	args := buildPodmanRunArgs(opts)
	cmd := buildCmd("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman run: %w", err)
	}
	return nil
}

func (e *PodmanEngine) StopContainer(name string) error {
	cmd := buildCmd("podman", "stop", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman stop: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *PodmanEngine) StartContainer(name string) error {
	cmd := buildCmd("podman", "start", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman start: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *PodmanEngine) RemoveContainer(name string) error {
	cmd := buildCmd("podman", "rm", "-f", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *PodmanEngine) ContainerStatus(name string) (string, error) {
	cmd := buildCmd("podman", "inspect", "--format", "{{.State.Status}}", name)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("podman inspect: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (e *PodmanEngine) TailLogs(name string, follow bool, tail int) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail), name)
	cmd := buildCmd("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Version returns the Podman version string.
func (e *PodmanEngine) Version() string {
	cmd := buildCmd("podman", "version", "--format", "{{.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// ImageDigest returns the repo digest of the given image.
func (e *PodmanEngine) ImageDigest(image string) string {
	cmd := buildCmd("podman", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildPodmanRunArgs builds args for `podman run`. It deliberately avoids
// --replace so a name collision can never remove an unrelated container.
func buildPodmanRunArgs(opts RunOptions) []string {
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

	// Map host port → container port 8080 for multi-instance support.
	args = append(args, "-p", hostPort+":8080")

	args = append(args,
		"-v", opts.HomeVolume+":/home/omnideck",
		"-v", opts.StateVolume+":/var/lib/omnideck",
	)

	// OLLAMA_HOST — always set. Podman resolves host.containers.internal when
	// it can determine a route back to the host; this is environment-dependent,
	// not a reliable major-version boundary.
	ollamaHost := opts.OllamaHost
	if ollamaHost == "" {
		if opts.Platform == "darwin" {
			ollamaHost = "host.docker.internal:11434"
		} else {
			ollamaHost = "host.containers.internal:11434"
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

// ContainerStats returns live CPU and memory stats for a running container.
func (e *PodmanEngine) ContainerStats(name string) (cpu string, cpuPct float64, ram, ramTotal string, ramPct float64, err error) {
	cmd := buildCmd("podman", "stats", "--no-stream",
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
	// Podman/Docker reports MemPerc as 0 when no memory limit is set.
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
func (e *PodmanEngine) ContainerInspect(name string) (InspectData, error) {
	format := `{{.State.StartedAt}}|{{.Created}}|{{.RestartCount}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}`
	cmd := buildCmd("podman", "inspect", "--format", format, name)
	out, err := cmd.Output()
	if err != nil {
		return InspectData{}, fmt.Errorf("podman inspect: %w", err)
	}
	return parseInspectLine(strings.TrimSpace(string(out)))
}

// FetchLogs returns the last tail lines of container log output (stdout + stderr).
func (e *PodmanEngine) FetchLogs(name string, tail int) ([]string, error) {
	cmd := buildCmd("podman", "logs", "--tail", fmt.Sprintf("%d", tail), "--timestamps", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("podman logs: %w", err)
	}
	raw := strings.Split(strings.TrimSpace(string(out)), "\n")
	return raw, nil
}

// Ensure PodmanEngine implements Engine at compile time.
var _ Engine = (*PodmanEngine)(nil)
