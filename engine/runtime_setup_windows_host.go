package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func ensureRuntimeHostPrerequisites(host HostPlatform, onEvent func(RuntimeSetupEvent)) error {
	if host.OS != "windows" {
		return nil
	}
	state, wsl, powershell := windowsWSL2State()
	switch state {
	case "ready":
		return nil
	case "restart":
		return windowsRestartRequiredError()
	}
	if wsl == "" || powershell == "" {
		return runtimeSetupError(
			RuntimeSetupSupport,
			"This computer cannot prepare Windows Subsystem for Linux 2.",
			"Install pending Windows updates and confirm this computer supports WSL 2, then try again.",
			nil,
		)
	}

	emitRuntimeSetup(onEvent, setupSubstageEvent(
		SetupStageSoftware,
		SetupSubstageWSLPermission,
		"permission",
		"Waiting for approval in Windows Security…",
		"Approval required",
		"Your computer will ask you to approve turning on Windows Subsystem for Linux, which omnideck needs to run in an isolated space. omnideck never sees or stores your password.",
	))
	script := windowsWSLInstallScript()
	err := runQuietSetupCommand(
		powershell,
		[]string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script},
		append(os.Environ(), "OMNIDECK_WSL_PATH="+wsl),
		[]int{0, 3010},
		func() {
			emitRuntimeSetup(onEvent, setupSubstageEvent(
				SetupStageSoftware,
				SetupSubstageWSLEnable,
				"progress",
				"Enabling Windows Subsystem for Linux…",
				"Enabling Windows features",
				"Windows is still enabling WSL 2. Leave setup running while it finishes.",
			))
		},
	)
	if err != nil {
		if exitCode(err) == 1223 {
			return runtimeSetupError(
				RuntimeSetupPermissionCancelled,
				"Windows approval wasn’t granted",
				"The Windows security prompt was cancelled. Try again and choose Yes to continue setup.",
				err,
			)
		}
		return runtimeSetupError(
			RuntimeSetupWindowsFeatures,
			"Windows features couldn’t be enabled",
			"Approval was granted, but Windows couldn’t finish enabling WSL. Install pending Windows updates, restart the computer, then try again.",
			err,
		)
	}

	state, _, _ = windowsWSL2State()
	return windowsWSLPostInstallError(state)
}

func windowsWSLInstallScript() string {
	return strings.Join([]string{
		"try { $process = Start-Process -FilePath $env:OMNIDECK_WSL_PATH -ArgumentList @('--install', '--no-distribution') -WindowStyle Hidden -Verb RunAs -Wait -PassThru; exit $process.ExitCode }",
		"catch { if ($_.Exception.HResult -eq 0x800704C7) { exit 1223 }; Write-Error $_.Exception.Message; exit 1 }",
	}, " ")
}

func windowsRestartRequiredError() error {
	return runtimeSetupError(
		RuntimeSetupRestart,
		"Windows must restart to finish enabling WSL 2.",
		"Save any open work, restart Windows, and omnideck will continue setup after you sign in.",
		nil,
	)
}

func windowsWSLPostInstallError(state string) error {
	switch state {
	case "ready":
		return nil
	case "restart":
		return windowsRestartRequiredError()
	default:
		return runtimeSetupError(
			RuntimeSetupWindowsFeatures,
			"Windows features couldn’t be enabled",
			"Approval was granted, but Windows couldn’t finish enabling WSL. Install pending Windows updates, restart the computer, then try again.",
			fmt.Errorf("WSL readiness check returned %q after installation", state),
		)
	}
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
	wslStatusCommand := exec.CommandContext(processCtx, wsl, "--status")
	prepareHiddenConsoleCommand(wslStatusCommand)
	if err := wslStatusCommand.Run(); err != nil {
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
	prepareHiddenConsoleCommand(command)
	if err := command.Run(); err == nil {
		return "ready", wsl, powershell
	} else if exitCode(err) == 2 {
		return "restart", wsl, powershell
	}
	return "missing", wsl, powershell
}

func installPodmanWindows(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent)) error {
	destination, err := downloadPodmanInstaller(host, downloadRoot, onEvent)
	if err != nil {
		return err
	}
	if err := verifyWindowsInstaller(destination); err != nil {
		return runtimeSetupError(RuntimeSetupDownload, "The Podman installer did not pass its security check.", "Delete the cached installer and try again.", err)
	}
	logPath := filepath.Join(downloadRoot, "podman-install.log")
	emitRuntimeSetup(onEvent, setupSubstageEvent(
		SetupStageSoftware,
		SetupSubstagePodmanInstall,
		"progress",
		"Installing Podman…",
		"Installer running",
		"",
	))
	err = runVisibleSetupCommand(
		"msiexec.exe",
		windowsMSIArguments(destination, logPath),
		os.Environ(),
		[]int{0, 3010},
		func() {
			emitRuntimeSetup(onEvent, setupSubstageEvent(
				SetupStageSoftware,
				SetupSubstagePodmanInstall,
				"progress",
				"Installing Podman…",
				"Installer still running",
				"",
			))
		},
	)
	logTail, logErr := retainBoundedLog(logPath)
	if err == nil {
		// Successful verbose logs have no diagnostic value. If removal fails,
		// retainBoundedLog has already capped the file at maxCommandOutput.
		_ = os.Remove(logPath)
		return nil
	}
	installerErr := fmt.Errorf("msiexec failed with exit code %d; installer log: %s: %w", exitCode(err), logPath, err)
	if detail := cleanCommandOutput(string(logTail)); detail != "" {
		installerErr = fmt.Errorf("%w\nInstaller log tail:\n%s", installerErr, detail)
	} else if logErr != nil {
		installerErr = fmt.Errorf("%w\nInstaller log could not be read: %v", installerErr, logErr)
	}
	return runtimeSetupError(
		RuntimeSetupInstaller,
		"Podman couldn’t be installed",
		"Restart Windows and try again. Technical details include the installer’s result.",
		installerErr,
	)
}

func windowsMSIArguments(destination, logPath string) []string {
	return []string{
		"/i", destination,
		"/quiet", "/norestart", "/l*v", logPath,
		"ALLUSERS=2", "MSIINSTALLPERUSER=1",
	}
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
	prepareHiddenConsoleCommand(command)
	command.Env = append(os.Environ(), "OMNIDECK_INSTALLER_PATH="+destination)
	return command.Run()
}
