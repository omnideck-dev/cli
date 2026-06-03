package smoke

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// binaryPath is set by TestMain after building the binary.
var binaryPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "omnideck-smoke-*")
	if err != nil {
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binaryPath = filepath.Join(dir, "omnideck"+ext)

	// Build from the module path so it works regardless of working directory.
	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.version=test -X main.commit=abc1234 -X main.date=2025-01-01",
		"-o", binaryPath,
		"github.com/omnideck-dev/cli",
	)
	cmd.Stderr = os.Stderr
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

func run(args ...string) (string, string, int) {
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = nil
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return stdout.String(), stderr.String(), code
}

// --- Tests ---

func TestVersionFlag(t *testing.T) {
	stdout, _, code := run("--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "omnideck version") {
		t.Fatalf("expected version output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "test") {
		t.Fatalf("expected ldflags version 'test' in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "abc1234") {
		t.Fatalf("expected commit 'abc1234' in output, got: %s", stdout)
	}
}

func TestHelpFlag(t *testing.T) {
	stdout, _, code := run("--help")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "omnideck") {
		t.Fatalf("expected help to mention omnideck, got: %s", stdout)
	}
	if !strings.Contains(stdout, "install") {
		t.Fatalf("expected help to list 'install' command, got: %s", stdout)
	}
}

func TestNoArgsShowsHelp(t *testing.T) {
	stdout, _, code := run()
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "omnideck") {
		t.Fatalf("expected help output with no args, got: %s", stdout)
	}
}

func TestSubcommandHelp(t *testing.T) {
	for _, sub := range []string{"install", "start", "stop", "restart", "status", "logs", "doctor", "config", "update", "uninstall"} {
		t.Run(sub, func(t *testing.T) {
			stdout, _, code := run(sub, "--help")
			if code != 0 {
				t.Fatalf("%s --help exited %d", sub, code)
			}
			if !strings.Contains(stdout, sub) {
				t.Fatalf("%s --help should mention %s, got: %s", sub, sub, stdout)
			}
		})
	}
}

func TestDebugFlag(t *testing.T) {
	_, _, code := run("--debug", "--version")
	if code != 0 {
		t.Fatalf("expected exit 0 with --debug, got %d", code)
	}
}

func TestNoColorFlag(t *testing.T) {
	_, _, code := run("--no-color", "--version")
	if code != 0 {
		t.Fatalf("expected exit 0 with --no-color, got %d", code)
	}
}

func TestInvalidCommand(t *testing.T) {
	_, stderr, code := run("nonexistent-command-xyz")
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid command")
	}
	// Cobra prints errors to stderr.
	if stderr == "" && code != 0 {
		// Some versions print to stdout; check combined output.
		t.Log("stderr was empty but exit code was non-zero (output may be on stdout)")
	}
}

func TestConfigFlagNonexistent(t *testing.T) {
	_, _, code := run("--config", "/nonexistent/path/config.yaml", "--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func TestBinaryIsExecutable(t *testing.T) {
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("binary is zero bytes")
	}
	if runtime.GOOS != "windows" {
		if info.Mode()&0111 == 0 {
			t.Fatal("binary is not executable")
		}
	}
}
