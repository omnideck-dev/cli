// Command releasecontract validates a packaged Omnideck CLI without importing
// implementation packages. It is intentionally a black-box consumer of the
// executable and its documented JSON contract.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const commandTimeout = 15 * time.Second

type options struct {
	binary          string
	archive         string
	mode            string
	expectedVersion string
	expectedOS      string
	expectedArch    string
	contractsDir    string
	reportPath      string
	junitPath       string
}

type checkResult struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	DurationMS int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

type testReport struct {
	Mode       string        `json:"mode"`
	Binary     string        `json:"binary"`
	Platform   string        `json:"platform"`
	StartedAt  string        `json:"startedAt"`
	FinishedAt string        `json:"finishedAt"`
	Passed     bool          `json:"passed"`
	Checks     []checkResult `json:"checks"`
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
	timedOut bool
}

func main() {
	opts := parseFlags()
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.binary, "binary", "", "path to an extracted omnideck executable")
	flag.StringVar(&opts.archive, "archive", "", "path to an omnideck release .zip or .tar.gz")
	flag.StringVar(&opts.mode, "mode", "portable", "artifact or portable")
	flag.StringVar(&opts.expectedVersion, "expected-version", "", "exact version expected from --json --version")
	flag.StringVar(&opts.expectedOS, "expected-os", runtime.GOOS, "expected Go operating system")
	flag.StringVar(&opts.expectedArch, "expected-arch", runtime.GOARCH, "expected Go architecture")
	flag.StringVar(&opts.contractsDir, "contracts", "contracts", "path to the checked-in machine-readable contracts")
	flag.StringVar(&opts.reportPath, "report", "", "optional JSON report path")
	flag.StringVar(&opts.junitPath, "junit", "", "optional JUnit XML report path")
	flag.Parse()
	return opts
}

func run(opts options) error {
	if (opts.binary == "") == (opts.archive == "") {
		return errors.New("specify exactly one of --binary or --archive")
	}
	if opts.mode != "artifact" && opts.mode != "portable" {
		return fmt.Errorf("--mode must be artifact or portable, got %q", opts.mode)
	}

	started := time.Now().UTC()
	binary := opts.binary
	cleanup := func() {}
	if opts.archive != "" {
		var err error
		binary, cleanup, err = extractReleaseArchive(opts.archive, opts.expectedOS)
		if err != nil {
			return err
		}
	}
	defer cleanup()

	report := testReport{
		Mode:      opts.mode,
		Binary:    filepath.Base(binary),
		Platform:  opts.expectedOS + "/" + opts.expectedArch,
		StartedAt: started.Format(time.RFC3339),
		Passed:    true,
	}

	addCheck := func(name string, fn func() error) {
		checkStarted := time.Now()
		err := fn()
		result := checkResult{Name: name, Passed: err == nil, DurationMS: time.Since(checkStarted).Milliseconds()}
		if err != nil {
			result.Detail = err.Error()
			report.Passed = false
		}
		report.Checks = append(report.Checks, result)
		status := "PASS"
		if err != nil {
			status = "FAIL"
		}
		fmt.Printf("%s  %s", status, name)
		if err != nil {
			fmt.Printf(": %v", err)
		}
		fmt.Println()
	}

	addCheck("binary-format-and-architecture", func() error {
		return inspectBinary(binary, opts.expectedOS, opts.expectedArch)
	})
	if info, err := os.Stat(binary); err != nil {
		addCheck("binary-is-nonempty", func() error { return err })
	} else {
		addCheck("binary-is-nonempty", func() error {
			if info.Size() == 0 {
				return errors.New("binary is empty")
			}
			return nil
		})
	}

	if opts.mode == "portable" {
		if opts.expectedOS != runtime.GOOS || opts.expectedArch != runtime.GOARCH {
			addCheck("native-platform", func() error {
				return fmt.Errorf("cannot execute %s/%s on %s/%s", opts.expectedOS, opts.expectedArch, runtime.GOOS, runtime.GOARCH)
			})
		} else {
			for _, check := range portableChecks(binary, opts.expectedVersion, opts.contractsDir) {
				check := check
				addCheck(check.name, check.run)
			}
		}
	}

	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := writeReports(report, opts.reportPath, opts.junitPath); err != nil {
		return err
	}
	if !report.Passed {
		return errors.New("release contract failed")
	}
	return nil
}

type namedCheck struct {
	name string
	run  func() error
}

func portableChecks(binary, expectedVersion, contractsDir string) []namedCheck {
	return []namedCheck{
		{name: "contract-schemas-compile", run: func() error { return checkContractSchemas(contractsDir) }},
		{name: "version-json", run: func() error { return checkVersionJSON(binary, expectedVersion, contractsDir) }},
		{name: "version-text", run: func() error { return checkVersionText(binary, expectedVersion) }},
		{name: "help-command-surface", run: func() error { return checkHelp(binary) }},
		{name: "subcommand-help", run: func() error { return checkSubcommandHelp(binary) }},
		{name: "bare-noninteractive-help", run: func() error { return checkBareNoninteractive(binary) }},
		{name: "bare-json-structured-error", run: func() error { return checkBareJSON(binary, contractsDir) }},
		{name: "empty-list-json", run: func() error { return checkEmptyList(binary) }},
		{name: "suggest-defaults-json", run: func() error { return checkSuggestedDefaults(binary) }},
		{name: "runtime-status-schema-4", run: func() error { return checkRuntimeStatus(binary, contractsDir) }},
		{name: "missing-instance-json", run: func() error { return checkMissingInstance(binary, contractsDir) }},
		{name: "invalid-invocations-stay-json", run: func() error { return checkInvalidInvocations(binary, contractsDir) }},
		{name: "ambiguous-instance-never-prompts", run: func() error { return checkAmbiguousInstances(binary, contractsDir) }},
		{name: "remove-requires-explicit-flags", run: func() error { return checkRemoveSafety(binary, contractsDir) }},
	}
}

func checkContractSchemas(contractsDir string) error {
	for _, relative := range []string{
		"json/v2/version.schema.json",
		"json/v2/error.schema.json",
		"json/v2/stage-event.schema.json",
		"runtime/v4/status.schema.json",
	} {
		if _, err := compileSchema(contractsDir, relative); err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
	}
	return nil
}

func checkVersionJSON(binary, expectedVersion, contractsDir string) error {
	result, err := invoke(binary, nil, "--json", "--version")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	if err := validateJSONSchema(contractsDir, "json/v2/version.schema.json", result.stdout); err != nil {
		return err
	}
	var payload struct {
		Version      string `json:"version"`
		Commit       string `json:"commit"`
		Date         string `json:"date"`
		JSONContract int    `json:"jsonContract"`
	}
	if err := decodeSingleJSON(result.stdout, &payload); err != nil {
		return err
	}
	if expectedVersion != "" && payload.Version != expectedVersion {
		return fmt.Errorf("version = %q, want %q", payload.Version, expectedVersion)
	}
	if payload.Version == "" || payload.Commit == "" || payload.Date == "" {
		return fmt.Errorf("incomplete version payload: %+v", payload)
	}
	if payload.JSONContract != 2 {
		return fmt.Errorf("jsonContract = %d, want 2", payload.JSONContract)
	}
	return nil
}

func checkVersionText(binary, expectedVersion string) error {
	result, err := invoke(binary, nil, "--no-color", "--version")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if !strings.Contains(result.stdout, "omnideck version") {
		return fmt.Errorf("version output is missing product name: %q", result.stdout)
	}
	if expectedVersion != "" && !strings.Contains(result.stdout, expectedVersion) {
		return fmt.Errorf("version output does not contain %q: %q", expectedVersion, result.stdout)
	}
	return nil
}

func checkHelp(binary string) error {
	result, err := invoke(binary, nil, "--no-color", "--help")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	for _, command := range []string{"add", "list", "status", "doctor", "environment", "runtime", "remove"} {
		if !strings.Contains(result.stdout, command) {
			return fmt.Errorf("help output is missing command %q", command)
		}
	}
	return nil
}

func checkSubcommandHelp(binary string) error {
	for _, command := range []string{"add", "list", "start", "stop", "restart", "status", "logs", "doctor", "config", "environment", "runtime", "update", "remove"} {
		result, err := invoke(binary, nil, command, "--help")
		if err != nil {
			return err
		}
		if err := requireExit(result, 0); err != nil {
			return fmt.Errorf("%s --help: %w", command, err)
		}
		if !strings.Contains(result.stdout, command) {
			return fmt.Errorf("%s --help does not identify its command", command)
		}
	}
	return nil
}

func checkBareNoninteractive(binary string) error {
	result, err := invoke(binary, nil)
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if !strings.Contains(result.stdout, "Usage:") {
		return fmt.Errorf("closed-stdin bare command did not print help: %q", result.stdout)
	}
	return nil
}

func checkBareJSON(binary, contractsDir string) error {
	result, err := invoke(binary, nil, "--json")
	if err != nil {
		return err
	}
	if result.exitCode == 0 {
		return errors.New("bare --json unexpectedly exited 0")
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	return requireErrorCode(result.stdout, "MISSING_SUBCOMMAND", contractsDir)
}

func checkEmptyList(binary string) error {
	result, err := invoke(binary, nil, "--json", "list")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	var entries []json.RawMessage
	if err := decodeSingleJSON(result.stdout, &entries); err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("empty isolated config returned %d instances", len(entries))
	}
	return nil
}

func checkSuggestedDefaults(binary string) error {
	result, err := invoke(binary, nil, "--json", "add", "--suggest-defaults")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	var payload struct {
		Name      string `json:"name"`
		WebUIPort string `json:"webUiPort"`
	}
	if err := decodeSingleJSON(result.stdout, &payload); err != nil {
		return err
	}
	if payload.Name == "" || payload.WebUIPort == "" {
		return fmt.Errorf("incomplete defaults payload: %+v", payload)
	}
	return nil
}

func checkRuntimeStatus(binary, contractsDir string) error {
	result, err := invoke(binary, nil, "--json", "runtime", "status")
	if err != nil {
		return err
	}
	if err := requireExit(result, 0); err != nil {
		return err
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	if err := validateJSONSchema(contractsDir, "runtime/v4/status.schema.json", result.stdout); err != nil {
		return err
	}
	var payload struct {
		SchemaVersion int    `json:"schemaVersion"`
		Runtime       string `json:"runtime"`
		State         string `json:"state"`
		Ready         bool   `json:"ready"`
		Phase         string `json:"phase"`
		Activity      string `json:"activity"`
		Resources     struct {
			Container struct {
				Memory  string `json:"memory"`
				SHMSize string `json:"shmSize"`
			} `json:"container"`
			Machine struct {
				Mode string `json:"mode"`
			} `json:"machine"`
		} `json:"resources"`
	}
	if err := decodeSingleJSON(result.stdout, &payload); err != nil {
		return err
	}
	if payload.SchemaVersion != 4 || payload.Runtime != "podman" {
		return fmt.Errorf("runtime identity = schema %d, runtime %q; want schema 4 and podman", payload.SchemaVersion, payload.Runtime)
	}
	if payload.State == "" || payload.Phase == "" || payload.Activity == "" {
		return fmt.Errorf("runtime status is missing state or phase metadata: %+v", payload)
	}
	if payload.Resources.Container.Memory == "" || payload.Resources.Container.SHMSize == "" || payload.Resources.Machine.Mode == "" {
		return fmt.Errorf("runtime status is missing resource policy: %+v", payload.Resources)
	}
	return nil
}

func checkMissingInstance(binary, contractsDir string) error {
	result, err := invoke(binary, nil, "--json", "--name", "release-contract-missing", "status")
	if err != nil {
		return err
	}
	if result.exitCode == 0 {
		return errors.New("missing instance status unexpectedly exited 0")
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	return requireErrorCode(result.stdout, "NOT_INSTALLED", contractsDir)
}

func checkInvalidInvocations(binary, contractsDir string) error {
	for _, args := range [][]string{
		{"--json", "definitely-not-a-command"},
		{"--json", "--definitely-not-a-flag"},
		{"--json", "remove"},
	} {
		result, err := invoke(binary, nil, args...)
		if err != nil {
			return err
		}
		if result.exitCode == 0 {
			return fmt.Errorf("invalid invocation unexpectedly exited 0: %v", args)
		}
		if err := requireJSONStderrEmpty(result); err != nil {
			return fmt.Errorf("%v: %w", args, err)
		}
		if err := requireErrorCode(result.stdout, "INTERNAL_ERROR", contractsDir); err != nil {
			return fmt.Errorf("%v: %w", args, err)
		}
	}
	return nil
}

func checkAmbiguousInstances(binary, contractsDir string) error {
	configDir, err := os.MkdirTemp("", "omnideck-release-contract-config-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(configDir)
	instances := filepath.Join(configDir, "instances")
	if err := os.MkdirAll(instances, 0o700); err != nil {
		return err
	}
	for index, name := range []string{"release-contract-one", "release-contract-two"} {
		fixture := fmt.Sprintf("container_name: %s\nmemory: 2g\nshm_size: 1g\nweb_ui_port: \"%d\"\nimage: example.invalid/fixture:never-run\n", name, 42340+index)
		if err := os.WriteFile(filepath.Join(instances, name+".yaml"), []byte(fixture), 0o600); err != nil {
			return err
		}
	}
	result, err := invoke(binary, []string{"OMNIDECK_CONFIG_DIR=" + configDir}, "--json", "status")
	if err != nil {
		return err
	}
	if result.exitCode == 0 {
		return errors.New("ambiguous status unexpectedly exited 0")
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	if err := validateJSONSchema(contractsDir, "json/v2/error.schema.json", result.stdout); err != nil {
		return err
	}
	var envelope struct {
		Error struct {
			Code      string   `json:"code"`
			Instances []string `json:"instances"`
		} `json:"error"`
	}
	if err := decodeSingleJSON(result.stdout, &envelope); err != nil {
		return err
	}
	if envelope.Error.Code != "AMBIGUOUS_INSTANCE" || len(envelope.Error.Instances) != 2 {
		return fmt.Errorf("unexpected ambiguous-instance payload: %+v", envelope.Error)
	}
	return nil
}

func checkRemoveSafety(binary, contractsDir string) error {
	configDir, err := os.MkdirTemp("", "omnideck-release-contract-config-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(configDir)
	instances := filepath.Join(configDir, "instances")
	if err := os.MkdirAll(instances, 0o700); err != nil {
		return err
	}
	fixture := []byte("container_name: release-contract-solo\nmemory: 2g\nshm_size: 1g\nweb_ui_port: \"42337\"\nimage: example.invalid/fixture:never-run\n")
	if err := os.WriteFile(filepath.Join(instances, "release-contract-solo.yaml"), fixture, 0o600); err != nil {
		return err
	}
	result, err := invoke(binary, []string{"OMNIDECK_CONFIG_DIR=" + configDir}, "--json", "remove", "release-contract-solo")
	if err != nil {
		return err
	}
	if result.exitCode == 0 {
		return errors.New("remove without explicit safety flags unexpectedly exited 0")
	}
	if err := requireJSONStderrEmpty(result); err != nil {
		return err
	}
	if err := requireErrorCode(result.stdout, "MISSING_REQUIRED_FLAG", contractsDir); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(instances, "release-contract-solo.yaml")); err != nil {
		return fmt.Errorf("remove safety check changed its fixture: %w", err)
	}
	return nil
}

func invoke(binary string, extraEnv []string, args ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	configDir, err := os.MkdirTemp("", "omnideck-release-contract-config-*")
	if err != nil {
		return commandResult{}, err
	}
	defer os.RemoveAll(configDir)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(), "OMNIDECK_CONFIG_DIR="+configDir, "NO_COLOR=1")
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		result.timedOut = true
		return result, fmt.Errorf("command timed out after %s: %v", commandTimeout, args)
	}
	if runErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("starting %s: %w", binary, runErr)
}

func requireExit(result commandResult, expected int) error {
	if result.exitCode != expected {
		return fmt.Errorf("exit = %d, want %d; stdout=%q stderr=%q", result.exitCode, expected, result.stdout, result.stderr)
	}
	return nil
}

func requireJSONStderrEmpty(result commandResult) error {
	if strings.TrimSpace(result.stderr) != "" {
		return fmt.Errorf("JSON command wrote to stderr: %q", result.stderr)
	}
	if strings.Contains(result.stdout, "\x1b[") {
		return errors.New("JSON stdout contains ANSI escapes")
	}
	return nil
}

func decodeSingleJSON(text string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON %q: %w", text, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("stdout contains more than one JSON value")
		}
		return fmt.Errorf("checking trailing JSON: %w", err)
	}
	return nil
}

func requireErrorCode(text, expected, contractsDir string) error {
	if err := validateJSONSchema(contractsDir, "json/v2/error.schema.json", text); err != nil {
		return err
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := decodeSingleJSON(text, &envelope); err != nil {
		return err
	}
	if envelope.Error.Code != expected {
		return fmt.Errorf("error code = %q, want %q", envelope.Error.Code, expected)
	}
	if strings.TrimSpace(envelope.Error.Message) == "" {
		return errors.New("structured error message is empty")
	}
	return nil
}

func validateJSONSchema(contractsDir, relative, text string) error {
	schema, err := compileSchema(contractsDir, relative)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return fmt.Errorf("decoding instance for %s: %w", relative, err)
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("output does not conform to %s: %w", relative, err)
	}
	return nil
}

func compileSchema(contractsDir, relative string) (*jsonschema.Schema, error) {
	path := filepath.Join(contractsDir, filepath.FromSlash(relative))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema %s: %w", path, err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decoding schema %s: %w", path, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", document); err != nil {
		return nil, fmt.Errorf("adding schema %s: %w", path, err)
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("compiling schema %s: %w", path, err)
	}
	return schema, nil
}

func extractReleaseArchive(path, expectedOS string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "omnideck-release-archive-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	expectedName := "omnideck"
	if expectedOS == "windows" {
		expectedName += ".exe"
	}
	destination := filepath.Join(dir, expectedName)

	switch {
	case strings.HasSuffix(strings.ToLower(path), ".zip"):
		err = extractZipBinary(path, expectedName, destination)
	case strings.HasSuffix(strings.ToLower(path), ".tar.gz"):
		err = extractTarGzBinary(path, expectedName, destination)
	default:
		err = fmt.Errorf("unsupported archive format: %s", path)
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if expectedOS != "windows" {
		if err := os.Chmod(destination, 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return destination, cleanup, nil
}

func extractZipBinary(path, expectedName, destination string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) != 1 || !safeArchiveName(reader.File[0].Name, expectedName) {
		return fmt.Errorf("release zip must contain exactly %q at its root", expectedName)
	}
	source, err := reader.File[0].Open()
	if err != nil {
		return err
	}
	defer source.Close()
	return copyExecutable(destination, source)
}

func extractTarGzBinary(path, expectedName, destination string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := 0
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if header.Typeflag != tar.TypeReg || !safeArchiveName(header.Name, expectedName) {
			return fmt.Errorf("release tarball must contain only %q at its root", expectedName)
		}
		files++
		if files > 1 {
			return fmt.Errorf("release tarball contains more than one file")
		}
		if err := copyExecutable(destination, reader); err != nil {
			return err
		}
	}
	if files != 1 {
		return fmt.Errorf("release tarball did not contain %q", expectedName)
	}
	return nil
}

func safeArchiveName(name, expected string) bool {
	return name == expected && filepath.Base(filepath.Clean(name)) == expected
}

func copyExecutable(destination string, source io.Reader) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func inspectBinary(path, expectedOS, expectedArch string) error {
	switch expectedOS {
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return fmt.Errorf("opening ELF: %w", err)
		}
		defer file.Close()
		want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[expectedArch]
		if want == 0 || file.Machine != want {
			return fmt.Errorf("ELF machine = %s, want %s", file.Machine, expectedArch)
		}
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("opening Mach-O: %w", err)
		}
		defer file.Close()
		want := map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[expectedArch]
		if want == 0 || file.Cpu != want {
			return fmt.Errorf("Mach-O CPU = %s, want %s", file.Cpu, expectedArch)
		}
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return fmt.Errorf("opening PE: %w", err)
		}
		defer file.Close()
		want := map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64}[expectedArch]
		if want == 0 || file.Machine != want {
			return fmt.Errorf("PE machine = %#x, want %s", file.Machine, expectedArch)
		}
	default:
		return fmt.Errorf("unsupported expected OS %q", expectedOS)
	}
	return nil
}

func writeReports(report testReport, jsonPath, junitPath string) error {
	if jsonPath != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := writeReportFile(jsonPath, append(data, '\n')); err != nil {
			return err
		}
	}
	if junitPath != "" {
		type failure struct {
			Message string `xml:"message,attr"`
		}
		type testcase struct {
			Name      string   `xml:"name,attr"`
			Classname string   `xml:"classname,attr"`
			Time      string   `xml:"time,attr"`
			Failure   *failure `xml:"failure,omitempty"`
		}
		type testsuite struct {
			XMLName  xml.Name   `xml:"testsuite"`
			Name     string     `xml:"name,attr"`
			Tests    int        `xml:"tests,attr"`
			Failures int        `xml:"failures,attr"`
			Cases    []testcase `xml:"testcase"`
		}
		suite := testsuite{Name: "omnideck-release-contract", Tests: len(report.Checks)}
		for _, check := range report.Checks {
			item := testcase{Name: check.Name, Classname: report.Platform, Time: fmt.Sprintf("%.3f", float64(check.DurationMS)/1000)}
			if !check.Passed {
				suite.Failures++
				item.Failure = &failure{Message: check.Detail}
			}
			suite.Cases = append(suite.Cases, item)
		}
		data, err := xml.MarshalIndent(suite, "", "  ")
		if err != nil {
			return err
		}
		data = append([]byte(xml.Header), data...)
		data = append(data, '\n')
		if err := writeReportFile(junitPath, data); err != nil {
			return err
		}
	}
	return nil
}

func writeReportFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
