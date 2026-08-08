package smoke

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
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

	// Find the module root (where go.mod lives) so we build local code,
	// not the cached module from the proxy.
	moduleRoot := findModuleRoot()

	cmd := exec.Command("go", "build",
		"-buildvcs=false",
		"-ldflags", "-X main.version=test -X main.commit=abc1234 -X main.date=2025-01-01",
		"-o", binaryPath,
		".",
	)
	cmd.Dir = moduleRoot
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

// findModuleRoot walks up the directory tree looking for go.mod.
func findModuleRoot() string {
	// Prefer GITHUB_WORKSPACE in CI.
	if ws := os.Getenv("GITHUB_WORKSPACE"); ws != "" {
		return ws
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
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
	if !strings.Contains(stdout, "setup") || !strings.Contains(stdout, "list") {
		t.Fatalf("expected help to list setup and list commands, got: %s", stdout)
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
	for _, sub := range []string{"add", "install", "setup", "list", "start", "stop", "restart", "status", "logs", "doctor", "config", "environment", "update", "remove", "uninstall"} {
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

func runEnv(env []string, args ...string) (string, string, int) {
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), env...)
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

func writeInstanceFixture(t *testing.T, configDir, name string) {
	t.Helper()
	t.Setenv("OMNIDECK_CONFIG_DIR", configDir)
	cfg := config.DefaultConfig()
	cfg.ContainerName = name
	if err := config.Save(config.InstancePath(name), cfg); err != nil {
		t.Fatalf("writing instance fixture %s: %v", name, err)
	}
}

func TestVersionJSON(t *testing.T) {
	stdout, _, code := run("--version", "--json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	var payload struct {
		Version      string `json:"version"`
		Commit       string `json:"commit"`
		Date         string `json:"date"`
		JSONContract int    `json:"jsonContract"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("--version --json did not produce valid JSON: %v\n%s", err, stdout)
	}
	if payload.JSONContract != 2 {
		t.Fatalf("jsonContract = %d, want 2", payload.JSONContract)
	}
	if payload.Version != "test" || payload.Commit != "abc1234" {
		t.Fatalf("unexpected version payload: %+v", payload)
	}
}

// TestBareJSONNeverLaunchesTUI guards the exact failure mode this whole spec
// exists to prevent: a GUI spawning `omnideck --json` with no subcommand
// must never fall through to a TUI attempt or plain-text help.
func TestBareJSONNeverLaunchesTUI(t *testing.T) {
	stdout, stderr, code := run("--json")
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0: %s", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("bare --json wrote Cobra error or usage text to stderr: %q", stderr)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("bare --json did not produce valid JSON on stdout: %v\n%s", err, stdout)
	}
	if payload.Error.Code != "MISSING_SUBCOMMAND" {
		t.Fatalf("error code = %q, want MISSING_SUBCOMMAND", payload.Error.Code)
	}
}

// TestJSONSyntaxErrorsStayMachineReadable covers failures Cobra detects before
// PersistentPreRun, when normal command-level JSON error helpers cannot run.
func TestJSONSyntaxErrorsStayMachineReadable(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "definitely-not-a-command"},
		{"--json", "--definitely-not-a-flag"},
		{"--json", "remove"},
	} {
		name := strings.Join(args[1:], " ")
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := run(args...)
			if code == 0 {
				t.Fatalf("expected non-zero exit, got 0: %s", stdout)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("JSON syntax error wrote Cobra text to stderr: %q", stderr)
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("syntax error did not produce valid JSON: %v\n%s", err, stdout)
			}
			if payload.Error.Code != "INTERNAL_ERROR" || payload.Error.Message == "" {
				t.Fatalf("unexpected syntax error payload: %+v", payload.Error)
			}
		})
	}
}

// TestJSONModeNeverHangsOnAmbiguousInstance is the non-interactive-safety
// regression guard from JSON_MODE_SPEC.md's Testing section: every command
// that would otherwise open the interactive picker must fail fast with the
// structured AMBIGUOUS_INSTANCE error under --json + closed stdin, never
// hang waiting on tui.RunPicker.
func TestJSONModeNeverHangsOnAmbiguousInstance(t *testing.T) {
	configDir := t.TempDir()
	writeInstanceFixture(t, configDir, "one")
	writeInstanceFixture(t, configDir, "two")
	env := []string{"OMNIDECK_CONFIG_DIR=" + configDir}

	cases := [][]string{
		{"status"}, {"start"}, {"stop"}, {"restart"}, {"logs"},
		{"update"}, {"doctor"},
		{"config", "show"}, {"config", "set", "memory", "2g"},
	}
	for _, args := range cases {
		sub := strings.Join(args, " ")
		t.Run(sub, func(t *testing.T) {
			done := make(chan struct{})
			var stdout string
			var code int
			go func() {
				stdout, _, code = runEnv(env, append(append([]string{}, args...), "--json")...)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Fatalf("%s --json hung with an ambiguous instance and no --name", sub)
			}
			if code == 0 {
				t.Fatalf("%s --json with an ambiguous instance should fail, got exit 0: %s", sub, stdout)
			}
			var payload struct {
				Error struct {
					Code      string   `json:"code"`
					Instances []string `json:"instances"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("%s --json did not produce valid JSON on stdout: %v\n%s", sub, err, stdout)
			}
			if payload.Error.Code != "AMBIGUOUS_INSTANCE" {
				t.Fatalf("%s error code = %q, want AMBIGUOUS_INSTANCE", sub, payload.Error.Code)
			}
			if len(payload.Error.Instances) != 2 {
				t.Fatalf("%s expected 2 instances listed, got %v", sub, payload.Error.Instances)
			}
		})
	}
}

// TestListJSONNeverAmbiguous asserts list --json is the one command
// explicitly about every instance: it must never return AMBIGUOUS_INSTANCE
// even with multiple saved instances and no --name.
func TestListJSONNeverAmbiguous(t *testing.T) {
	configDir := t.TempDir()
	writeInstanceFixture(t, configDir, "one")
	writeInstanceFixture(t, configDir, "two")
	env := []string{"OMNIDECK_CONFIG_DIR=" + configDir}

	stdout, _, code := runEnv(env, "list", "--json")
	if code != 0 {
		t.Fatalf("list --json exited %d: %s", code, stdout)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("list --json did not produce a JSON array: %v\n%s", err, stdout)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %s", len(entries), stdout)
	}
}

// TestDoctorJSONShape sanity-checks doctor --json's end-to-end wiring (no
// saved instance, so it exercises the "not set up yet" branch without
// needing a real container).
func TestDoctorJSONShape(t *testing.T) {
	configDir := t.TempDir()
	env := []string{"OMNIDECK_CONFIG_DIR=" + configDir}

	stdout, _, _ := runEnv(env, "doctor", "--json")
	var payload struct {
		Checks []struct {
			Label  string `json:"label"`
			Status string `json:"status"`
		} `json:"checks"`
		AllPass bool `json:"allPass"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("doctor --json did not produce valid JSON: %v\n%s", err, stdout)
	}
	if len(payload.Checks) == 0 {
		t.Fatal("expected at least one check")
	}
	for _, c := range payload.Checks {
		switch c.Status {
		case "pass", "fail", "warn", "info":
		default:
			t.Fatalf("check %q has unexpected status %q", c.Label, c.Status)
		}
	}
}

// TestSuggestDefaultsJSON exercises `add --suggest-defaults --json`, which
// must work without any container runtime — it only reads saved config.
func TestSuggestDefaultsJSON(t *testing.T) {
	configDir := t.TempDir()
	env := []string{"OMNIDECK_CONFIG_DIR=" + configDir}

	stdout, _, code := runEnv(env, "add", "--suggest-defaults", "--json")
	if code != 0 {
		t.Fatalf("add --suggest-defaults --json exited %d: %s", code, stdout)
	}
	var payload struct {
		Name      string `json:"name"`
		WebUIPort string `json:"webUiPort"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if payload.Name == "" || payload.WebUIPort == "" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

// TestRemoveJSONRequiresExplicitFlags is the end-to-end guard for
// JSON_MODE_SPEC.md's "no silent default for a destructive choice": remove
// --json with none of --yes/--keep-volumes/--delete-volumes set must fail
// fast with MISSING_REQUIRED_FLAG, never prompt.
func TestRemoveJSONRequiresExplicitFlags(t *testing.T) {
	configDir := t.TempDir()
	writeInstanceFixture(t, configDir, "solo")
	env := []string{"OMNIDECK_CONFIG_DIR=" + configDir}

	stdout, stderr, code := runEnv(env, "remove", "solo", "--json")
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0: %s", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("remove --json wrote Cobra error or usage text to stderr: %q", stderr)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("remove --json did not produce valid JSON on stdout: %v\n%s", err, stdout)
	}
	if payload.Error.Code != "MISSING_REQUIRED_FLAG" {
		t.Fatalf("error code = %q, want MISSING_REQUIRED_FLAG", payload.Error.Code)
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
