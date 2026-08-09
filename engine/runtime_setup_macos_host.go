package engine

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const macOSAuthorizationCancelledMarker = "OMNIDECK_AUTHORIZATION_CANCELLED"

func installPodmanDarwin(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent)) error {
	destination, err := downloadPodmanInstaller(host, downloadRoot, onEvent)
	if err != nil {
		return err
	}
	if err := exec.CommandContext(processCtx, "/usr/sbin/pkgutil", "--check-signature", destination).Run(); err != nil {
		return runtimeSetupError(RuntimeSetupDownload, "The Podman installer did not pass its security check.", "Delete the cached installer and try again.", err)
	}
	emitRuntimeSetup(onEvent, setupSubstageEvent(
		SetupStageSoftware,
		SetupSubstageMacPermission,
		"permission",
		"Waiting for approval from macOS…",
		"Waiting for approval",
		"Your Mac will ask you to approve installing Podman. omnideck never sees or stores your password.",
	))
	var installStarted sync.Once
	emitInstallStarted := func() {
		installStarted.Do(func() {
			emitRuntimeSetup(onEvent, setupSubstageEvent(
				SetupStageSoftware,
				SetupSubstagePodmanInstall,
				"progress",
				"Installing Podman…",
				"Writing application files",
				"",
			))
		})
	}
	err = runObservedQuietSetupCommand(
		"/usr/bin/osascript",
		[]string{"-e", macOSInstallerScript(), destination},
		os.Environ(),
		[]int{0},
		func() {
			if macOSInstallerRunning(destination) {
				emitInstallStarted()
			}
		},
	)
	if macOSAuthorizationCancelled(err) {
		return runtimeSetupError(
			RuntimeSetupPermissionCancelled,
			"macOS approval wasn’t granted",
			"The macOS password prompt was cancelled. Try again and approve it to continue setup.",
			err,
		)
	}
	// A very fast installation can finish between process observations. Emit the
	// truthful install stage before reporting its result, but never do this for a
	// cancelled authorization prompt.
	emitInstallStarted()
	if err != nil {
		return runtimeSetupError(RuntimeSetupInstaller, "Podman couldn’t be installed", "Try again and approve the macOS prompt. Technical details include the installer’s result.", err)
	}
	return nil
}

func macOSInstallerScript() string {
	return `on run argv
try
do shell script "/usr/sbin/installer -pkg " & quoted form of item 1 of argv & " -target /" with administrator privileges
on error errorMessage number errorNumber
if errorNumber is -128 then
error "OMNIDECK_AUTHORIZATION_CANCELLED" number -128
end if
error errorMessage number errorNumber
end try
end run`
}

func macOSAuthorizationCancelled(err error) bool {
	if err == nil {
		return false
	}
	detail := err.Error()
	return strings.Contains(detail, macOSAuthorizationCancelledMarker) || strings.Contains(detail, "(-128)")
}

func macOSInstallerRunning(destination string) bool {
	observationContext, cancel := context.WithTimeout(processCtx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(observationContext, "/bin/ps", "-axo", "command=")
	prepareHiddenConsoleCommand(command)
	output, err := command.Output()
	return err == nil && macOSInstallerProcessVisible(string(output), destination)
}

func macOSInstallerProcessVisible(processes, destination string) bool {
	for _, process := range strings.Split(processes, "\n") {
		if strings.Contains(process, "osascript") {
			continue
		}
		if strings.Contains(process, "/usr/sbin/installer") && strings.Contains(process, destination) {
			return true
		}
	}
	return false
}
