package engine

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/omnideck-dev/cli/cmd/debug"
)

const maxCommandOutput = 64 * 1024

// buildCmd builds a Podman command tied to the CLI's cancellation context.
// Keeping command construction here gives every caller the same PATH refresh,
// cancellation, and debug behavior.
func buildCmd(binary string, args ...string) *exec.Cmd {
	prepareRuntimeCommand(binary)
	if binary == "podman" {
		args = podmanCommandArgs(runtime.GOOS, args...)
	}
	if debug.Enabled() {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", binary, strings.Join(args, " "))
	}
	command := exec.CommandContext(processCtx, binary, args...)
	command.Env = hostCommandEnvironment(os.Environ())
	prepareHiddenConsoleCommand(command)
	return command
}

// RunSetupCommand executes one command from a SetupPlan and streams its
// line-oriented progress to onLine. Desktop and TUI setup both consume plans
// built by this package, so the command, arguments, PATH refresh, and
// cancellation behavior stay identical.
func RunSetupCommand(command SetupCommand, onLine func(string)) error {
	messages := make(chan string)
	done := make(chan error, 1)
	go func() {
		done <- streamCommandOutput(command.Display, buildCmd(command.Name, command.Args...), messages)
		close(messages)
	}()
	for line := range messages {
		if onLine != nil {
			onLine(line)
		}
	}
	return <-done
}

// commandOutput runs a command whose stdout is structured data and preserves
// stderr when the command fails. exec.Cmd.Output does capture stderr in some
// cases, but wrapping only its exit error loses that explanation.
func commandOutput(action string, cmd *exec.Cmd) ([]byte, error) {
	var stderr tailBuffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, runtimeCommandError(action, err, joinCommandOutput(out, stderr.Bytes()))
	}
	return out, nil
}

// commandCombinedOutput runs a command whose successful output is not streamed
// and consistently turns engine failures into reportable errors.
func commandCombinedOutput(action string, cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, runtimeCommandError(action, err, out)
	}
	return out, nil
}

// streamCommandOutput forwards line-oriented stdout while retaining a bounded
// tail of stdout and stderr for a useful error if the command fails. Pull
// progress can be large, so retaining only the tail avoids unbounded memory.
func streamCommandOutput(action string, cmd *exec.Cmd, msgs chan<- string) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s: reading command output: %w", action, err)
	}
	var stdoutTail, stderrTail tailBuffer
	cmd.Stderr = &stderrTail
	if err := cmd.Start(); err != nil {
		return runtimeCommandError(action, err, stderrTail.Bytes())
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = stdoutTail.Write([]byte(line + "\n"))
		if msgs != nil {
			msgs <- line
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if waitErr != nil {
		return runtimeCommandError(action, waitErr, joinCommandOutput(stdoutTail.Bytes(), stderrTail.Bytes()))
	}
	if scanErr != nil {
		return fmt.Errorf("%s: reading command output: %w", action, scanErr)
	}
	return nil
}

// runtimeCommandError keeps the engine's explanation alongside its exit code.
// Full-screen callers cannot safely stream stderr directly to the terminal.
func runtimeCommandError(action string, err error, output []byte) error {
	detail := cleanCommandOutput(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w (the container engine did not provide more details)", action, err)
	}
	return fmt.Errorf("%s: %w\n%s", action, err, detail)
}

func cleanCommandOutput(output string) string {
	output = ansi.Strip(output)
	output = strings.ReplaceAll(output, "\r", "\n")
	lines := strings.Split(output, "\n")
	cleaned := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}

func joinCommandOutput(parts ...[]byte) []byte {
	var joined tailBuffer
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(joined.Bytes()) > 0 {
			_, _ = joined.Write([]byte("\n"))
		}
		_, _ = joined.Write(part)
	}
	return joined.Bytes()
}

func volumeNotFound(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no such volume") ||
		strings.Contains(lower, "no volume with name or id") ||
		strings.Contains(lower, "volume not found")
}

// tailBuffer implements io.Writer while retaining only the most recent output.
type tailBuffer struct {
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if len(p) >= maxCommandOutput {
		b.data = append(b.data[:0], p[len(p)-maxCommandOutput:]...)
		return written, nil
	}
	b.data = append(b.data, p...)
	if extra := len(b.data) - maxCommandOutput; extra > 0 {
		copy(b.data, b.data[extra:])
		b.data = b.data[:maxCommandOutput]
	}
	return written, nil
}

func (b *tailBuffer) Bytes() []byte {
	return b.data
}
