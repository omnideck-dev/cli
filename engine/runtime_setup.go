package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	PodmanInstallerVersion = "v6.0.2"
	SetupStageSoftware     = "software"
	SetupStageEnvironment  = "environment"

	SetupActivitySoftware    = "Getting your computer ready…"
	SetupActivityEnvironment = "Preparing a secure space to run in…"
)

type RuntimeSetupFailure string

const (
	RuntimeSetupComponents  RuntimeSetupFailure = "components"
	RuntimeSetupPermission  RuntimeSetupFailure = "permission"
	RuntimeSetupDownload    RuntimeSetupFailure = "downloads"
	RuntimeSetupEnvironment RuntimeSetupFailure = "environment"
	RuntimeSetupRestart     RuntimeSetupFailure = "restart"
	RuntimeSetupSupport     RuntimeSetupFailure = "support"
)

// RuntimeSetupEvent is the shared setup progress contract consumed by both the
// TUI and desktop. Stage and activity use the desktop setup vocabulary.
type RuntimeSetupEvent struct {
	Stage    string
	State    string
	Activity string
	Detail   string
	Progress *float64
}

// RuntimeSetupError carries the user-facing failure category and next step
// without requiring either front end to classify command output.
type RuntimeSetupError struct {
	Failure RuntimeSetupFailure
	Message string
	Hint    string
	Err     error
}

func (e *RuntimeSetupError) Error() string { return e.Message }
func (e *RuntimeSetupError) Unwrap() error { return e.Err }

// RuntimeSetupOptions configures the shared prerequisite and runtime setup.
// The private seams are populated only by unit tests in this package.
type RuntimeSetupOptions struct {
	Host                   HostPlatform
	DownloadRoot           string
	OnEvent                func(RuntimeSetupEvent)
	AllowTerminalElevation bool

	probe      func() ProbeResult
	ensureHost func(HostPlatform, func(RuntimeSetupEvent)) error
	install    func(HostPlatform, string, func(RuntimeSetupEvent)) error
	runCommand func(SetupCommand, func(string)) error
}

type podmanInstaller struct {
	Version  string
	Filename string
	SHA256   string
}

var podmanInstallers = map[string]podmanInstaller{
	"darwin-amd64": {
		Version:  "v5.8.5",
		Filename: "podman-installer-macos-amd64.pkg",
		SHA256:   "2677be9fa3bf75f7dbd4dfe5f5039cf105806f102af15ee6c6d174c70dcda3b8",
	},
	"darwin-arm64": {
		Version:  PodmanInstallerVersion,
		Filename: "podman-installer-macos-arm64.pkg",
		SHA256:   "5a1d97f98f626cdb82dbd9932cf43102d1e9b6621627085fec2dcadf59743930",
	},
	"windows-amd64": {
		Version:  PodmanInstallerVersion,
		Filename: "podman-installer-windows-amd64.msi",
		SHA256:   "c094059880f033656092f5fb4306457e42aa068ee32137162299817c5f79396f",
	},
	"windows-arm64": {
		Version:  PodmanInstallerVersion,
		Filename: "podman-installer-windows-arm64.msi",
		SHA256:   "9f6bb7fb83acbfb13cbf67a40f407f098b2f3181a294e3264da260c49437437a",
	},
}

func emitRuntimeSetup(onEvent func(RuntimeSetupEvent), event RuntimeSetupEvent) {
	if onEvent != nil {
		onEvent(event)
	}
}

func setupActivity(stage string) string {
	if stage == SetupStageEnvironment {
		return SetupActivityEnvironment
	}
	return SetupActivitySoftware
}

func setupEvent(stage, state, detail string) RuntimeSetupEvent {
	return RuntimeSetupEvent{
		Stage:    stage,
		State:    state,
		Activity: setupActivity(stage),
		Detail:   detail,
	}
}

func setupProgress(stage, detail string, fraction float64) RuntimeSetupEvent {
	event := setupEvent(stage, "progress", detail)
	if fraction >= 0 {
		bounded := max(0, min(1, fraction))
		event.Progress = &bounded
	}
	return event
}

func runtimeSetupError(failure RuntimeSetupFailure, message, hint string, err error) error {
	return &RuntimeSetupError{Failure: failure, Message: message, Hint: hint, Err: err}
}

// EnsureRuntime installs host prerequisites and Podman when needed, prepares
// the shared omnideck-runtime machine on macOS/Windows, and verifies readiness.
func EnsureRuntime(options RuntimeSetupOptions) (ProbeResult, error) {
	host := options.Host
	if host.OS == "" {
		host = DetectHostPlatform()
	}
	probe := options.probe
	if probe == nil {
		probe = func() ProbeResult {
			probes := ProbeAll()
			if len(probes) == 0 {
				return ProbeResult{Name: "podman", State: RuntimeMissing}
			}
			return probes[0]
		}
	}
	ensureHost := options.ensureHost
	if ensureHost == nil {
		ensureHost = ensureRuntimeHostPrerequisites
	}
	install := options.install
	if install == nil {
		install = func(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent)) error {
			return installPodmanRuntime(host, downloadRoot, onEvent, options.AllowTerminalElevation)
		}
	}
	runCommand := options.runCommand
	if runCommand == nil {
		runCommand = RunSetupCommand
	}
	downloadRoot := options.DownloadRoot

	current := probe()
	if current.Ready() {
		return current, nil
	}

	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageSoftware, "start", ""))
	if err := ensureHost(host, options.OnEvent); err != nil {
		return current, err
	}

	if current.State == RuntimeMissing || current.State == RuntimeUnsupportedVersion {
		if downloadRoot == "" {
			cacheRoot, err := os.UserCacheDir()
			if err != nil {
				return current, runtimeSetupError(
					RuntimeSetupComponents,
					"omnideck could not prepare a download location.",
					"Check that your account can write to its local application-data folder, then try again.",
					err,
				)
			}
			downloadRoot = filepath.Join(cacheRoot, "omnideck-cli", "downloads")
		}
		if err := install(host, downloadRoot, options.OnEvent); err != nil {
			return current, err
		}
		current = probe()
		if current.State == RuntimeMissing {
			return current, runtimeSetupError(
				RuntimeSetupComponents,
				"Podman was installed but omnideck still cannot open it.",
				"Restart this terminal and try again. If Podman is still missing, restart the computer.",
				nil,
			)
		}
	}
	emitRuntimeSetup(options.OnEvent, setupProgress(SetupStageSoftware, "", 1))
	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageSoftware, "done", ""))

	if current.Ready() {
		return current, nil
	}

	plans := BuildSetupPlans([]ProbeResult{current}, host)
	if len(plans) == 0 || len(plans[0].Commands) == 0 {
		return current, runtimeSetupError(
			RuntimeSetupEnvironment,
			"The secure workspace needs attention before omnideck can use it.",
			"Restart the computer, then try setup again. If it still fails, open diagnostics for the Podman details.",
			nil,
		)
	}

	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageEnvironment, "start", ""))
	commands := plans[0].Commands
	for i, command := range commands {
		err := runCommand(command, func(line string) {
			fraction := float64(i) / float64(len(commands))
			emitRuntimeSetup(options.OnEvent, setupProgress(SetupStageEnvironment, line, fraction))
		})
		if err != nil {
			return current, runtimeSetupError(
				RuntimeSetupEnvironment,
				"The secure workspace could not be prepared.",
				"Restart the computer and try again. Anything already downloaded will be reused.",
				err,
			)
		}
		emitRuntimeSetup(options.OnEvent, setupProgress(
			SetupStageEnvironment,
			"",
			float64(i+1)/float64(len(commands)),
		))
	}

	current = probe()
	if !current.Ready() {
		return current, runtimeSetupError(
			RuntimeSetupEnvironment,
			"The secure workspace was prepared but is not responding.",
			"Restart the computer and try again. If it still fails, open diagnostics for the Podman details.",
			nil,
		)
	}
	emitRuntimeSetup(options.OnEvent, setupEvent(SetupStageEnvironment, "done", ""))
	return current, nil
}

func ensureRuntimeHostPrerequisites(host HostPlatform, onEvent func(RuntimeSetupEvent)) error {
	if host.OS != "windows" {
		return nil
	}
	state, wsl, powershell := windowsWSL2State()
	switch state {
	case "ready":
		return nil
	case "restart":
		return runtimeSetupError(
			RuntimeSetupRestart,
			"Windows must restart to finish enabling WSL 2.",
			"Save any open work, restart Windows, and omnideck will continue setup after you sign in.",
			nil,
		)
	}
	if wsl == "" || powershell == "" {
		return runtimeSetupError(
			RuntimeSetupSupport,
			"This computer cannot prepare Windows Subsystem for Linux 2.",
			"Install pending Windows updates and confirm this computer supports WSL 2, then try again.",
			nil,
		)
	}

	emitRuntimeSetup(onEvent, setupEvent(
		SetupStageSoftware,
		"permission",
		"Your computer will ask you to approve turning on Windows Subsystem for Linux, which omnideck needs to run in an isolated space. omnideck never sees or stores your password.",
	))
	script := strings.Join([]string{
		"$process = Start-Process -FilePath $env:OMNIDECK_WSL_PATH -ArgumentList @('--install', '--no-distribution') -Verb RunAs -Wait -PassThru",
		"exit $process.ExitCode",
	}, "; ")
	err := runQuietSetupCommand(
		powershell,
		[]string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script},
		append(os.Environ(), "OMNIDECK_WSL_PATH="+wsl),
		[]int{0, 3010},
		func() {
			emitRuntimeSetup(onEvent, setupEvent(
				SetupStageSoftware,
				"waiting",
				"Windows is still enabling WSL 2. Leave its setup window open. If the percentage has not changed for 10 minutes, cancel that setup, restart Windows, then try again.",
			))
		},
	)
	if err != nil {
		return runtimeSetupError(
			RuntimeSetupPermission,
			"omnideck needs your permission to turn on WSL 2.",
			"Try again and approve the request from Windows.",
			err,
		)
	}

	state, _, _ = windowsWSL2State()
	if state != "ready" {
		return runtimeSetupError(
			RuntimeSetupRestart,
			"Windows must restart to finish enabling WSL 2.",
			"Save any open work, restart Windows, and omnideck will continue setup after you sign in.",
			nil,
		)
	}
	return nil
}

const windowsWSL2ReadinessScript = "$feature = Get-CimInstance -ClassName Win32_OptionalFeature | Where-Object -Property Name -EQ -Value VirtualMachinePlatform; $computer = Get-CimInstance -ClassName Win32_ComputerSystem; $restartPending = Test-Path 'HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Component Based Servicing\\RebootPending'; $vmCompute = Get-Service -Name vmcompute -ErrorAction SilentlyContinue; if ($feature.InstallState -ne 1) { exit 1 }; if ($restartPending -or $null -eq $vmCompute) { exit 2 }; if ($computer.HypervisorPresent) { exit 0 }; exit 1"

func windowsWSL2State() (state, wsl, powershell string) {
	if runtime.GOOS != "windows" {
		return "missing", "", ""
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = os.Getenv("WINDIR")
	}
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	wsl = filepath.Join(systemRoot, "System32", "wsl.exe")
	if _, err := os.Stat(wsl); err != nil {
		if wsl, err = exec.LookPath("wsl.exe"); err != nil {
			wsl = ""
		}
	}
	powershell, _ = exec.LookPath("powershell.exe")
	if wsl == "" {
		return "missing", wsl, powershell
	}
	if err := exec.CommandContext(processCtx, wsl, "--status").Run(); err != nil {
		return "missing", wsl, powershell
	}
	if powershell == "" {
		return "missing", wsl, powershell
	}
	command := exec.CommandContext(
		processCtx,
		powershell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", windowsWSL2ReadinessScript,
	)
	if err := command.Run(); err == nil {
		return "ready", wsl, powershell
	} else if exitCode(err) == 2 {
		return "restart", wsl, powershell
	}
	return "missing", wsl, powershell
}

func installPodmanRuntime(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent), allowTerminalElevation bool) error {
	if host.OS == "linux" {
		return installPodmanLinux(host, onEvent, allowTerminalElevation)
	}
	installer, ok := podmanInstallers[host.OS+"-"+host.Arch]
	if !ok {
		return runtimeSetupError(
			RuntimeSetupSupport,
			"This release does not include Podman for this computer architecture.",
			"Check the supported-systems documentation for an available build.",
			nil,
		)
	}
	if err := os.MkdirAll(downloadRoot, 0o700); err != nil {
		return runtimeSetupError(RuntimeSetupDownload, "The Podman download could not be saved.", "Check available disk space and try again.", err)
	}
	destination := filepath.Join(downloadRoot, installer.Filename)
	url := fmt.Sprintf(
		"https://github.com/podman-container-tools/podman/releases/download/%s/%s",
		installer.Version,
		installer.Filename,
	)
	if err := downloadVerifiedFile(url, destination, installer.SHA256, func(fraction float64) {
		emitRuntimeSetup(onEvent, setupProgress(SetupStageSoftware, "Downloading required software…", fraction))
	}); err != nil {
		return runtimeSetupError(
			RuntimeSetupDownload,
			"The required software download did not finish.",
			"Check your internet connection and try again. Anything already downloaded will be reused.",
			err,
		)
	}

	switch host.OS {
	case "windows":
		if err := verifyWindowsInstaller(destination); err != nil {
			return runtimeSetupError(RuntimeSetupDownload, "The Podman installer did not pass its security check.", "Delete the cached installer and try again.", err)
		}
		emitRuntimeSetup(onEvent, setupEvent(
			SetupStageSoftware,
			"permission",
			"Your computer will ask you to approve installing Podman — the software omnideck uses to run in an isolated space. omnideck never sees or stores your password.",
		))
		err := runQuietSetupCommand(
			"msiexec.exe",
			[]string{"/i", destination, "/passive", "/norestart", "ALLUSERS=2", "MSIINSTALLPERUSER=1"},
			os.Environ(),
			[]int{0, 3010},
			func() {
				emitRuntimeSetup(onEvent, setupEvent(
					SetupStageSoftware,
					"waiting",
					"Podman is still installing. Leave the Windows installer open while it finishes. If its progress has not changed for 10 minutes, cancel it, restart Windows, then try again.",
				))
			},
		)
		if err != nil {
			return runtimeSetupError(RuntimeSetupComponents, "Podman could not be installed.", "Restart Windows and try again. If the installer opens, leave it open until it finishes.", err)
		}
	case "darwin":
		if err := exec.CommandContext(processCtx, "/usr/sbin/pkgutil", "--check-signature", destination).Run(); err != nil {
			return runtimeSetupError(RuntimeSetupDownload, "The Podman installer did not pass its security check.", "Delete the cached installer and try again.", err)
		}
		emitRuntimeSetup(onEvent, setupEvent(
			SetupStageSoftware,
			"permission",
			"Your Mac will ask you to approve installing Podman. omnideck never sees or stores your password.",
		))
		script := "on run argv\ndo shell script \"/usr/sbin/installer -pkg \" & quoted form of item 1 of argv & \" -target /\" with administrator privileges\nend run"
		if err := runQuietSetupCommand(
			"/usr/bin/osascript",
			[]string{"-e", script, destination},
			os.Environ(),
			[]int{0},
			func() {
				emitRuntimeSetup(onEvent, setupEvent(
					SetupStageSoftware,
					"waiting",
					"Podman is waiting to finish installing. Look for the macOS password prompt and approve it.",
				))
			},
		); err != nil {
			return runtimeSetupError(RuntimeSetupPermission, "omnideck needs your permission to install Podman.", "Try again and approve the request from macOS.", err)
		}
	}
	return nil
}

var (
	errLinuxPackageManagerMissing = errors.New("Linux package manager is missing")
	errLinuxElevationMissing      = errors.New("Linux privilege escalation is unavailable")
)

func installPodmanLinux(host HostPlatform, onEvent func(RuntimeSetupEvent), allowTerminalElevation bool) error {
	commands := podmanLinuxPackageCommands(host)
	if len(commands) == 0 {
		return runtimeSetupError(RuntimeSetupSupport, "Automatic Podman installation is not available for this Linux distribution.", "Install Podman with your distribution's package manager, then try again.", nil)
	}
	commands, err := prepareLinuxInstallCommands(commands, effectiveUserID(), allowTerminalElevation, exec.LookPath)
	if err != nil {
		if errors.Is(err, errLinuxPackageManagerMissing) {
			return runtimeSetupError(
				RuntimeSetupSupport,
				"This Linux installation does not include the expected software manager.",
				"Install Podman with this distribution's supported method, then try omnideck again.",
				err,
			)
		}
		if !allowTerminalElevation {
			return runtimeSetupError(
				RuntimeSetupPermission,
				"omnideck could not open your system's software-install permission prompt.",
				"Open a terminal, run omnideck once to finish installing Podman, then reopen the desktop app.",
				err,
			)
		}
		return runtimeSetupError(
			RuntimeSetupPermission,
			"omnideck cannot ask this account for permission to install Podman.",
			"Run omnideck from a desktop session with pkexec or sudo available, then try again.",
			err,
		)
	}
	for _, command := range commands {
		emitRuntimeSetup(onEvent, setupEvent(SetupStageSoftware, "permission", "Your computer may ask you to approve installing Podman."))
		if err := RunSetupCommand(command, func(line string) {
			emitRuntimeSetup(onEvent, setupProgress(SetupStageSoftware, line, -1))
		}); err != nil {
			return runtimeSetupError(RuntimeSetupComponents, "Podman could not be installed.", "Check your package manager and internet connection, then try again.", err)
		}
	}
	return nil
}

// prepareLinuxInstallCommands keeps distro package-manager selection separate
// from privilege escalation. Root runs the package manager directly; desktop
// sessions prefer the graphical PolicyKit prompt, and terminal-only sessions
// use sudo. No shell is involved in any path.
func prepareLinuxInstallCommands(
	commands []SetupCommand,
	effectiveUID int,
	allowTerminalElevation bool,
	lookPath func(string) (string, error),
) ([]SetupCommand, error) {
	for _, command := range commands {
		if _, err := lookPath(command.Name); err != nil {
			return nil, fmt.Errorf("%w: required command %s was not found: %v", errLinuxPackageManagerMissing, command.Name, err)
		}
	}
	if effectiveUID == 0 {
		return commands, nil
	}

	elevationName := "pkexec"
	elevationPath, err := lookPath(elevationName)
	if err != nil && allowTerminalElevation {
		elevationName = "sudo"
		elevationPath, err = lookPath(elevationName)
	}
	if err != nil {
		return nil, errLinuxElevationMissing
	}

	prepared := make([]SetupCommand, 0, len(commands))
	for _, packageCommand := range commands {
		args := append([]string{packageCommand.Name}, packageCommand.Args...)
		prepared = append(prepared, SetupCommand{
			Name:    elevationPath,
			Args:    args,
			Display: elevationName + " " + packageCommand.Display,
		})
	}
	return prepared, nil
}

func downloadVerifiedFile(url, destination, expectedSHA256 string, onProgress func(float64)) error {
	if digest, err := fileSHA256(destination); err == nil && digest == expectedSHA256 {
		if onProgress != nil {
			onProgress(1)
		}
		return nil
	}
	partial := destination + ".partial"
	_ = os.Remove(partial)
	request, err := http.NewRequestWithContext(processCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download failed with HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	reader := io.TeeReader(response.Body, hash)
	buffer := make([]byte, 128*1024)
	var received int64
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if _, err := file.Write(buffer[:count]); err != nil {
				file.Close()
				_ = os.Remove(partial)
				return err
			}
			received += int64(count)
			if onProgress != nil && response.ContentLength > 0 {
				onProgress(float64(received) / float64(response.ContentLength))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			_ = os.Remove(partial)
			return readErr
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		_ = os.Remove(partial)
		return errors.New("downloaded file does not match its reviewed SHA-256 digest")
	}
	_ = os.Remove(destination)
	return os.Rename(partial, destination)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyWindowsInstaller(destination string) error {
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return err
	}
	script := "$signature = Get-AuthenticodeSignature -LiteralPath $env:OMNIDECK_INSTALLER_PATH; if ($signature.Status -ne 'Valid') { exit 1 }"
	command := exec.CommandContext(
		processCtx,
		powershell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script,
	)
	command.Env = append(os.Environ(), "OMNIDECK_INSTALLER_PATH="+destination)
	return command.Run()
}

func runQuietSetupCommand(name string, args, environment []string, accepted []int, onWait func()) error {
	command := exec.CommandContext(processCtx, name, args...)
	command.Env = environment
	done := make(chan error, 1)
	go func() { done <- command.Run() }()
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err == nil || containsInt(accepted, exitCode(err)) {
			return nil
		}
		return err
	case <-timer.C:
		if onWait != nil {
			onWait()
		}
		err := <-done
		if err == nil || containsInt(accepted, exitCode(err)) {
			return nil
		}
		return err
	case <-processCtx.Done():
		return context.Cause(processCtx)
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
