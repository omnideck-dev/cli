package engine

import (
	"errors"
	"fmt"
	"os/exec"
)

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
	for commandIndex, command := range commands {
		if effectiveUserID() != 0 && commandIndex == 0 {
			emitRuntimeSetup(onEvent, setupSubstageEvent(
				SetupStageSoftware,
				"linux-permission",
				"permission",
				"Waiting for approval from your computer…",
				"Password required",
				"Your computer will ask you to approve installing Podman — the software omnideck uses to run in an isolated space. omnideck never sees or stores your password.",
			))
		}
		// Fedora and the apt family have an explicit package-index step. For
		// other supported managers the first command is the install itself.
		substage := SetupSubstagePackageInstall
		if len(commands) > 1 && commandIndex == 0 {
			substage = SetupSubstagePackageIndex
		}
		emitRuntimeSetup(onEvent, setupSubstageEvent(
			SetupStageSoftware,
			substage,
			"progress",
			map[bool]string{true: "Checking available software packages…", false: "Installing Podman…"}[substage == SetupSubstagePackageIndex],
			map[bool]string{true: "Package manager running", false: "Configuring dependencies"}[substage == SetupSubstagePackageIndex],
			"",
		))
		if err := RunSetupCommand(command, func(line string) {
			emitRuntimeSetup(onEvent, setupSubstageProgress(
				SetupStageSoftware,
				substage,
				map[bool]string{true: "Checking available software packages…", false: "Installing Podman…"}[substage == SetupSubstagePackageIndex],
				map[bool]string{true: "Package manager running", false: "Configuring dependencies"}[substage == SetupSubstagePackageIndex],
				line,
				-1,
			))
		}); err != nil {
			failure := RuntimeSetupInstaller
			message := "Podman couldn’t be installed"
			hint := "Check your package manager and available disk space, then try again."
			if substage == SetupSubstagePackageIndex {
				failure = RuntimeSetupPackageIndex
				message = "Available software couldn’t be checked"
				hint = "Check your package manager and internet connection, then try again."
			}
			return runtimeSetupError(failure, message, hint, err)
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
