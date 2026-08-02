package engine

import (
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
	out, err := commandOutput("podman ps", cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == name, nil
}

func (e *PodmanEngine) ImageExists(image string) (bool, error) {
	cmd := buildCmd("podman", "image", "exists", image)
	if err := cmd.Run(); err != nil {
		// Absent is reported by exit status, and is not a failure to ask.
		return false, nil
	}
	return true, nil
}

func (e *PodmanEngine) CreateVolume(name string) error {
	return createVolumeIfMissing(name, e.VolumeExists, func() error {
		cmd := buildCmd("podman", "volume", "create", name)
		_, err := commandCombinedOutput("podman volume create", cmd)
		return err
	})
}

// createVolumeIfMissing makes Podman setup safe to retry. Podman 3 does not
// support `volume create --ignore`, and unlike Docker it returns an error when
// a named volume already exists.
func createVolumeIfMissing(name string, exists func(string) (bool, error), create func() error) error {
	found, err := exists(name)
	if err != nil {
		return fmt.Errorf("checking volume %q: %w", name, err)
	}
	if found {
		return nil
	}
	if err := create(); err != nil {
		// Another setup process can create the same volume after our check.
		// Treat that race as success without hiding unrelated create failures.
		if found, inspectErr := exists(name); inspectErr == nil && found {
			return nil
		}
		return err
	}
	return nil
}

func (e *PodmanEngine) VolumeExists(name string) (bool, error) {
	cmd := buildCmd("podman", "volume", "inspect", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok && volumeNotFound(string(out)) {
			return false, nil
		}
		return false, runtimeCommandError("podman volume inspect", err, out)
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
	runPull := func(args ...string) error {
		cmd := buildCmd("podman", args...)
		return streamCommandOutput("podman pull", cmd, msgs)
	}
	return pullPodmanImage(image, msgs, runPull)
}

func pullPodmanImage(image string, msgs chan<- string, runPull func(...string) error) error {
	err := runPull("pull", image)
	if err == nil || !emptyDockerCredentialHelperError(err) {
		return err
	}

	// Podman falls back to Docker's credential configuration when it has no
	// matching native login. A stale empty helper entry makes even public pulls
	// fail by asking the OS to run "docker-credential-". Retry only that exact
	// malformed case with an explicit empty auth file. This prevents the
	// fallback without editing the user's Docker settings or ignoring a real,
	// named credential helper.
	authFile, cleanup, authErr := newAnonymousRegistryAuthFile()
	if authErr != nil {
		return fmt.Errorf("%w\nOmnideck could not prepare a safe retry without the invalid Docker login setting: %v", err, authErr)
	}
	defer cleanup()

	if msgs != nil {
		msgs <- "Podman found an invalid leftover Docker login setting. Retrying without it."
	}
	if retryErr := runPull("pull", "--authfile", authFile, image); retryErr != nil {
		return fmt.Errorf("Podman found an invalid empty Docker credential-helper setting; the retry without that setting also failed: %w", retryErr)
	}
	return nil
}

func emptyDockerCredentialHelperError(err error) bool {
	if err == nil {
		return false
	}
	detail := strings.ToLower(err.Error())
	return strings.Contains(detail, "error getting credentials") &&
		strings.Contains(detail, `"docker-credential-"`)
}

func newAnonymousRegistryAuthFile() (string, func(), error) {
	f, err := os.CreateTemp("", "omnideck-registry-auth-*.json")
	if err != nil {
		return "", func() {}, err
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.WriteString("{}\n"); err != nil {
		_ = f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func (e *PodmanEngine) RunContainer(opts RunOptions) error {
	args := buildPodmanRunArgs(opts)
	cmd := buildCmd("podman", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return runtimeCommandError("podman run", err, out)
	}
	return nil
}

func (e *PodmanEngine) CheckOllamaConnection(name string) error {
	return checkContainerOllama("podman", name)
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

func (e *PodmanEngine) TailLogs(name string, follow bool, tail int, stdout, stderr io.Writer) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail), name)
	cmd := buildCmd("podman", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
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
		"--log-driver=k8s-file",
		"--log-opt=max-size=150mb",
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
	ollamaHost := normalizeOllamaURL(opts.OllamaHost, "podman", opts.Platform)
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
