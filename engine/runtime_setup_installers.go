package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

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

func installPodmanRuntime(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent), allowTerminalElevation bool) error {
	switch host.OS {
	case "linux":
		return installPodmanLinux(host, onEvent, allowTerminalElevation)
	case "windows":
		return installPodmanWindows(host, downloadRoot, onEvent)
	case "darwin":
		return installPodmanDarwin(host, downloadRoot, onEvent)
	default:
		return runtimeSetupError(
			RuntimeSetupSupport,
			"This release does not include Podman for this computer architecture.",
			"Check the supported-systems documentation for an available build.",
			nil,
		)
	}
}

func downloadPodmanInstaller(host HostPlatform, downloadRoot string, onEvent func(RuntimeSetupEvent)) (string, error) {
	installer, ok := podmanInstallers[host.OS+"-"+host.Arch]
	if !ok {
		return "", runtimeSetupError(
			RuntimeSetupSupport,
			"This release does not include Podman for this computer architecture.",
			"Check the supported-systems documentation for an available build.",
			nil,
		)
	}
	if err := os.MkdirAll(downloadRoot, 0o700); err != nil {
		return "", runtimeSetupError(RuntimeSetupDownload, "The Podman download could not be saved.", "Check available disk space and try again.", err)
	}
	destination := filepath.Join(downloadRoot, installer.Filename)
	url := fmt.Sprintf(
		"https://github.com/podman-container-tools/podman/releases/download/%s/%s",
		installer.Version,
		installer.Filename,
	)
	emitRuntimeSetup(onEvent, setupSubstageProgress(
		SetupStageSoftware,
		SetupSubstagePodmanDownload,
		"Downloading Podman…",
		"Starting download",
		"",
		-1,
	))
	if err := downloadVerifiedFile(url, destination, installer.SHA256, func(progress verifiedDownloadProgress) {
		status := "Downloading required software"
		if progress.Total > 0 {
			status = fmt.Sprintf("%s of %s", formatDownloadBytes(progress.Received), formatDownloadBytes(progress.Total))
		} else if progress.Received > 0 {
			status = formatDownloadBytes(progress.Received) + " downloaded"
		}
		emitRuntimeSetup(onEvent, setupSubstageProgress(
			SetupStageSoftware,
			SetupSubstagePodmanDownload,
			"Downloading Podman…",
			status,
			"",
			progress.Fraction,
		))
	}); err != nil {
		return "", runtimeSetupError(
			RuntimeSetupDownload,
			"The required software download did not finish.",
			"Check your internet connection and try again. Anything already downloaded will be reused.",
			err,
		)
	}
	return destination, nil
}

func formatDownloadBytes(count int64) string {
	const (
		kilobyte = int64(1000)
		megabyte = int64(1000 * 1000)
	)
	switch {
	case count >= megabyte:
		return fmt.Sprintf("%.1f MB", float64(count)/float64(megabyte))
	case count >= kilobyte:
		return fmt.Sprintf("%.1f KB", float64(count)/float64(kilobyte))
	default:
		return fmt.Sprintf("%d bytes", count)
	}
}
