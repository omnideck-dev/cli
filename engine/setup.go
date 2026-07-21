package engine

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// HostPlatform contains only the host facts used to choose a safe setup path.
type HostPlatform struct {
	OS       string
	Arch     string
	DistroID string
	Version  string
	WSL      bool
	Systemd  bool
}

// SetupCommand is executed directly, without a shell, so arguments remain
// visible and no downloaded script is piped into a command that can change the
// computer's installed software.
type SetupCommand struct {
	Name    string
	Args    []string
	Display string
}

// SetupPlan describes one guided path to make a runtime usable.
type SetupPlan struct {
	Runtime           string
	State             RuntimeState
	Title             string
	Action            string
	Description       string
	Steps             []string
	PermissionNote    string
	Recommendation    string
	Commands          []SetupCommand
	URL               string
	DirectDownload    bool
	Recommended       bool
	RequiresElevation bool
	RequiresRestart   bool
	Manual            bool
	SafetyNote        string
}

// DetectHostPlatform reads os-release on Linux and otherwise relies on GOOS.
func DetectHostPlatform() HostPlatform {
	host := HostPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if host.OS != "linux" {
		return host
	}
	if procVersion, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(procVersion))
		host.WSL = strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		host.Systemd = true
	}

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return host
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	host.DistroID = values["ID"]
	host.Version = values["VERSION_ID"]
	return host
}

// RecommendedRuntime returns the easiest fresh-install default for a platform.
// Existing usable or repairable runtimes take precedence in BuildSetupPlans.
func RecommendedRuntime(host HostPlatform) string {
	if host.OS == "windows" || host.WSL {
		return "docker"
	}
	if host.OS == "darwin" && host.Arch == "amd64" {
		return "docker"
	}
	return "podman"
}

// BuildSetupPlans creates an ordered set of recovery/install choices. The
// recommendation prefers an already-installed runtime, then the platform default.
func BuildSetupPlans(probes []ProbeResult, host HostPlatform) []SetupPlan {
	plans := make([]SetupPlan, 0, len(probes))
	for _, probe := range probes {
		if probe.Ready() {
			continue
		}
		plans = append(plans, explainSetupPlan(setupPlanFor(probe, host), probe, host))
	}

	recommended := RecommendedRuntime(host)
	for i := range plans {
		if isSimpleRecovery(probeState(probes, plans[i].Runtime)) {
			plans[i].Recommended = true
			plans[i].Recommendation = plans[i].Title + " is already installed, so fixing it is the quickest option."
			return plans
		}
	}
	for i := range plans {
		if plans[i].Runtime == recommended {
			plans[i].Recommended = true
			plans[i].Recommendation = freshRecommendation(host, plans[i].Runtime)
			break
		}
	}
	return plans
}

func freshRecommendation(host HostPlatform, runtimeName string) string {
	if runtimeName == "docker" && (host.OS == "windows" || host.WSL) {
		return "Docker Desktop provides the most guided setup on Windows."
	}
	if runtimeName == "podman" && host.OS == "darwin" {
		return "Podman is a free option with an installer provided by the Podman project."
	}
	if runtimeName == "docker" && host.OS == "darwin" && host.Arch == "amd64" {
		return "Docker provides a current installer for Intel Macs."
	}
	if runtimeName == "podman" {
		return "Podman is a free option designed to run without giving a background program full control of your computer."
	}
	return "This is the simplest option for this computer."
}

func explainSetupPlan(plan SetupPlan, probe ProbeResult, host HostPlatform) SetupPlan {
	plan.Action = "Set up " + plan.Title
	switch probe.State {
	case RuntimeMachineMissing:
		plan.Action = "Finish setting up Podman"
		plan.Description = "Podman is installed, but its one-time setup is not finished."
		plan.Steps = []string{
			"Prepare Podman's private workspace. Podman may download files it needs.",
			"Start Podman.",
			"Check that Omnideck can use it.",
		}
	case RuntimeMachineStopped:
		plan.Action = "Start Podman"
		plan.Description = "Podman is installed and only needs to be started."
		plan.Steps = []string{"Start Podman.", "Check that Omnideck can use it."}
	case RuntimeStopped:
		plan.Action = "Start " + plan.Title
		plan.Description = plan.Title + " is installed and only needs to be started."
		plan.Steps = []string{"Start " + plan.Title + ".", "Wait until it is ready.", "Check that Omnideck can use it."}
	case RuntimePermissionDenied:
		plan.Action = "Open help for " + plan.Title
		plan.Description = plan.Title + " is installed, but your account is not allowed to use it."
		plan.Steps = []string{
			"Open the official help page.",
			"Follow its account-access steps.",
			"Return to Omnideck and check again.",
		}
	case RuntimeUnsupportedVersion:
		plan.Action = "Update Docker"
		plan.Description = "Docker is installed, but this version is too old for Omnideck."
		plan.Steps = []string{"Open Docker's official update instructions.", "Update Docker.", "Return to Omnideck and check again."}
	case RuntimeBroken:
		plan.Action = "Open help for " + plan.Title
		plan.Description = plan.Title + " is installed, but it is not working yet."
		plan.Steps = []string{"Open the official help page.", "Follow the steps for starting or repairing " + plan.Title + ".", "Return to Omnideck and check again."}
	case RuntimeMissing:
		explainMissingPlan(&plan, host)
	}

	if plan.RequiresElevation {
		if len(plan.Commands) > 0 && strings.Contains(plan.Commands[0].Display, "systemctl") {
			plan.PermissionNote = "Your computer may ask for your account password before it starts Docker. The password gives your computer permission to make this change. Omnideck does not see or store it."
		} else {
			plan.PermissionNote = "Your computer may ask for your account password before installing Podman. The password gives your computer's built-in installer permission to add the app. Omnideck does not see or store it."
		}
	}
	return plan
}

func explainMissingPlan(plan *SetupPlan, host HostPlatform) {
	plan.Action = "Install " + plan.Title
	if plan.Runtime == "podman" && host.OS == "linux" && len(plan.Commands) > 0 {
		plan.Description = "Install Podman, the free option recommended for this computer."
		plan.Steps = []string{
			"Ask your computer for the latest list of available apps.",
			"Download and install Podman.",
			"Check that Omnideck can use it.",
		}
		return
	}
	if plan.Runtime == "podman" && host.OS == "darwin" {
		if host.Arch == "amd64" {
			plan.Description = "Podman's current installer is not available for Intel Macs. Docker is the simplest choice."
			plan.Steps = []string{
				"Open Podman's official installation page.",
				"Review its current guidance for older Intel Macs.",
				"Return to Omnideck and check again if you install Podman.",
			}
			plan.SafetyNote = "Podman no longer provides its newest Mac installer for Intel Macs. Choose Docker unless you already know that an older Podman release works on this computer."
			return
		}
		plan.Description = "Download Podman's official Mac installer."
		plan.Steps = []string{
			"Wait for the official Podman installer download to finish.",
			"Open podman-installer-macos-arm64.pkg from Downloads and follow the instructions on screen.",
			"Return to Omnideck and check again.",
		}
		plan.DirectDownload = true
		plan.PermissionNote = "The Podman installer may ask for your Mac password so macOS can add the app. Omnideck does not see or store it."
		return
	}
	if plan.Runtime == "podman" && host.OS == "windows" {
		plan.Description = "Download Podman's official Windows installer."
		plan.Steps = []string{
			"Wait for the official Podman installer download to finish.",
			"Open the .msi installer from Downloads and keep the recommended Just for me choice.",
			"Return to Omnideck and check again. Omnideck will finish Podman's one-time setup.",
		}
		plan.DirectDownload = true
		plan.PermissionNote = "Installing Podman just for your account does not require an administrator. Podman also needs Windows 11 and either WSL 2 or Hyper-V. Turning on either Windows feature requires an administrator and may require a restart."
		plan.SafetyNote = "The Podman installer does not turn on WSL 2 or Hyper-V. If neither feature is already available, ask the person who manages this computer to enable one before Omnideck finishes Podman setup."
		return
	}
	if plan.Runtime == "docker" && (host.OS == "windows" || host.WSL) {
		if host.OS == "windows" && host.Arch == "arm64" {
			plan.Description = "Install Docker Desktop from Docker's official Windows page."
			plan.Steps = []string{
				"Open Docker's official Windows installation page.",
				"Download the ARM installer and follow the instructions on screen.",
				"Open Docker Desktop, return to Omnideck, and check again.",
			}
		} else {
			plan.Description = "Install Docker Desktop from its official Microsoft Store page."
			plan.Steps = []string{
				"Open Docker Desktop in Microsoft Store.",
				"Select Install and wait for it to finish.",
				"Open Docker Desktop, return to Omnideck, and check again.",
			}
		}
		plan.PermissionNote = "Installing Docker Desktop itself normally does not require an administrator. Windows may still ask for permission if it needs to turn on WSL, the Windows feature Docker uses to run containers."
		return
	}
	if plan.Runtime == "docker" && host.OS == "darwin" {
		plan.Description = "Open Docker Desktop's official download page and follow its Mac installer."
		plan.Steps = []string{
			"Open Docker's official download page.",
			"Download the installer for your Mac and follow the instructions on screen.",
			"Start Docker Desktop, return to Omnideck, and check again.",
		}
		plan.PermissionNote = "The Docker installer may ask for your Mac password so macOS can add the app. Omnideck does not see or store it."
		plan.SafetyNote = "Docker Desktop can require a paid subscription at larger companies. If this is a work computer, ask your company whether it already provides Docker Desktop."
		return
	}
	plan.Description = "Open the official download page and follow the instructions for this computer."
	plan.Steps = []string{
		"Open the official download page.",
		"Follow the installation instructions for your computer.",
		"Return to Omnideck and check again.",
	}
}

func isSimpleRecovery(state RuntimeState) bool {
	return state == RuntimeStopped || state == RuntimeMachineMissing || state == RuntimeMachineStopped
}

func probeState(probes []ProbeResult, name string) RuntimeState {
	for _, probe := range probes {
		if probe.Name == name {
			return probe.State
		}
	}
	return RuntimeMissing
}

func setupPlanFor(probe ProbeResult, host HostPlatform) SetupPlan {
	plan := SetupPlan{
		Runtime:     probe.Name,
		State:       probe.State,
		Title:       titleName(probe.Name),
		Description: RuntimeStateLabel(probe.State),
	}

	switch probe.State {
	case RuntimeMachineMissing:
		plan.Description = "Finish Podman's one-time setup"
		plan.Commands = []SetupCommand{command("podman", "machine", "init", "--now", "--update-connection=true")}
		return plan
	case RuntimeMachineStopped:
		plan.Description = "Start Podman"
		plan.Commands = []SetupCommand{command("podman", "machine", "start")}
		return plan
	case RuntimeStopped:
		return stoppedPlan(plan, host)
	case RuntimePermissionDenied:
		plan.Manual = true
		if probe.Name == "podman" {
			plan.Description = "Get help using Podman"
			plan.URL = troubleshootingURL("podman")
			return plan
		}
		plan.Description = "Get help using Docker"
		switch host.OS {
		case "windows":
			plan.URL = "https://docs.docker.com/desktop/setup/install/windows-permission-requirements/"
			plan.SafetyNote = "The person who manages this computer may need to give your account access to Docker. Omnideck will not change account security settings for you."
		case "darwin":
			plan.URL = troubleshootingURL("docker")
		default:
			if host.WSL {
				plan.URL = "https://docs.docker.com/desktop/features/wsl/"
				plan.SafetyNote = "First check that Docker Desktop allows the Linux environment you are using now to use Docker. Omnideck will not change account security settings for you."
			} else {
				plan.URL = "https://docs.docker.com/engine/install/linux-postinstall/"
				plan.SafetyNote = "Giving an account access to Docker can also give programs full control of this computer. Omnideck will not change that security setting for you."
			}
		}
		return plan
	case RuntimeUnsupportedVersion:
		plan.Description = "Update Docker"
		plan.Manual = true
		plan.URL = dockerInstallURL(host)
		return plan
	case RuntimeBroken:
		plan.Description = "Open official help for this app"
		plan.Manual = true
		plan.URL = troubleshootingURL(probe.Name)
		return plan
	default:
		return missingPlan(plan, host)
	}
}

func stoppedPlan(plan SetupPlan, host HostPlatform) SetupPlan {
	switch plan.Runtime {
	case "podman":
		if host.OS == "darwin" || host.OS == "windows" {
			plan.Description = "Start Podman"
			plan.Commands = []SetupCommand{command("podman", "machine", "start")}
			return plan
		}
		plan.Description = "Get help starting Podman"
		plan.Manual = true
		plan.URL = troubleshootingURL("podman")
	case "docker":
		if host.WSL {
			plan.Description = "Start Docker Desktop on Windows"
			plan.RequiresRestart = true
			plan.Commands = []SetupCommand{startDockerDesktopCommand()}
			return plan
		}
		switch host.OS {
		case "linux":
			if host.Systemd {
				plan.Description = "Start Docker"
				plan.RequiresElevation = true
				plan.Commands = []SetupCommand{command("sudo", "systemctl", "start", "docker")}
			} else {
				plan.Description = "Get help starting Docker"
				plan.Manual = true
				plan.URL = troubleshootingURL("docker")
				plan.SafetyNote = "Omnideck could not safely tell how this computer starts background apps, so it will show Docker's official help instead of guessing."
			}
		case "darwin":
			plan.Description = "Start Docker Desktop"
			plan.Commands = []SetupCommand{command("open", "-a", "Docker")}
			plan.RequiresRestart = true
		case "windows":
			plan.Description = "Start Docker Desktop"
			plan.RequiresRestart = true
			plan.Commands = []SetupCommand{startDockerDesktopCommand()}
		}
	}
	return plan
}

func missingPlan(plan SetupPlan, host HostPlatform) SetupPlan {
	if plan.Runtime == "podman" {
		switch host.OS {
		case "linux":
			plan.Description = "Install Podman"
			plan.Commands, plan.RequiresElevation = podmanLinuxCommands(host)
			if len(plan.Commands) == 0 {
				plan.Manual = true
				plan.URL = "https://podman.io/docs/installation"
			}
		case "darwin":
			plan.Description = "Install Podman with its official Mac installer"
			if host.Arch == "amd64" {
				plan.URL = "https://podman.io/docs/installation"
			} else {
				plan.URL = "https://github.com/containers/podman/releases/latest/download/podman-installer-macos-arm64.pkg"
			}
			plan.Manual = true
		case "windows":
			plan.Description = "Install Podman with its official Windows installer"
			arch := "amd64"
			if host.Arch == "arm64" {
				arch = "arm64"
			}
			plan.URL = "https://github.com/containers/podman/releases/latest/download/podman-installer-windows-" + arch + ".msi"
			plan.DirectDownload = true
			plan.Manual = true
		}
		return plan
	}

	plan.URL = dockerInstallURL(host)
	plan.Manual = true
	switch host.OS {
	case "windows":
		plan.Description = "Install Docker Desktop using its recommended Windows setup"
		plan.SafetyNote = "Windows may ask whether the installer is allowed to make changes to this computer. Docker Desktop can require a paid subscription at larger companies, so ask your company before installing it on a work computer."
	case "darwin":
		plan.Description = "Install Docker Desktop"
	case "linux":
		if host.WSL {
			plan.Description = "Install Docker Desktop on Windows and let this Linux environment use it"
			plan.SafetyNote = "Windows may ask whether the installer is allowed to make changes to this computer. Docker Desktop can require a paid subscription at larger companies, so ask your company before installing it on a work computer."
		} else {
			plan.Description = "Install Docker using its official instructions"
			plan.SafetyNote = "Docker setup differs across Linux versions. Omnideck will open Docker's official instructions instead of guessing which system changes are safe for this computer."
		}
	}
	return plan
}

func startDockerDesktopCommand() SetupCommand {
	return SetupCommand{
		Name: "powershell.exe",
		Args: []string{"-NoProfile", "-Command", `$paths = @("$Env:LOCALAPPDATA\Programs\DockerDesktop\Docker Desktop.exe", "$Env:ProgramFiles\Docker\Docker\Docker Desktop.exe"); ` +
			`$app = $paths | Where-Object { Test-Path $_ } | Select-Object -First 1; ` +
			`if (-not $app) { throw "Docker Desktop was not found" }; Start-Process $app`},
		Display: "Start Docker Desktop",
	}
}

func podmanLinuxCommands(host HostPlatform) ([]SetupCommand, bool) {
	switch host.DistroID {
	case "ubuntu":
		if host.Version != "" && !versionAtLeast(host.Version, 20, 10) {
			return nil, false
		}
		return []SetupCommand{
			command("sudo", "apt-get", "update"),
			command("sudo", "apt-get", "install", "-y", "podman"),
		}, true
	case "debian":
		if host.Version != "" && !versionAtLeast(host.Version, 11, 0) {
			return nil, false
		}
		return []SetupCommand{
			command("sudo", "apt-get", "update"),
			command("sudo", "apt-get", "install", "-y", "podman"),
		}, true
	case "linuxmint", "pop":
		return []SetupCommand{
			command("sudo", "apt-get", "update"),
			command("sudo", "apt-get", "install", "-y", "podman"),
		}, true
	case "fedora", "rhel", "centos", "rocky", "almalinux":
		return []SetupCommand{command("sudo", "dnf", "install", "-y", "podman")}, true
	case "arch", "manjaro":
		return []SetupCommand{command("sudo", "pacman", "-S", "--needed", "podman")}, true
	case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
		return []SetupCommand{command("sudo", "zypper", "install", "-y", "podman")}, true
	case "alpine":
		return []SetupCommand{command("sudo", "apk", "add", "podman")}, true
	default:
		return nil, false
	}
}

func versionAtLeast(value string, wantMajor, wantMinor int) bool {
	parts := strings.SplitN(value, ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major > wantMajor || (major == wantMajor && minor >= wantMinor)
}

func command(name string, args ...string) SetupCommand {
	return SetupCommand{Name: name, Args: args, Display: strings.Join(append([]string{name}, args...), " ")}
}

func titleName(name string) string {
	if name == "podman" {
		return "Podman"
	}
	return "Docker"
}

func dockerInstallURL(host HostPlatform) string {
	if host.WSL {
		return "ms-windows-store://pdp/?ProductId=XP8CBJ40XLBWKX"
	}
	switch host.OS {
	case "windows":
		if host.Arch != "arm64" {
			return "ms-windows-store://pdp/?ProductId=XP8CBJ40XLBWKX"
		}
		return "https://docs.docker.com/desktop/setup/install/windows-install/"
	case "darwin":
		return "https://docs.docker.com/desktop/setup/install/mac-install/"
	default:
		distro := host.DistroID
		switch distro {
		case "linuxmint", "pop":
			distro = "ubuntu"
		case "rocky", "almalinux":
			distro = "rhel"
		}
		if distro != "" {
			return fmt.Sprintf("https://docs.docker.com/engine/install/%s/", distro)
		}
		return "https://docs.docker.com/engine/install/"
	}
}

func troubleshootingURL(name string) string {
	if name == "podman" {
		return "https://podman.io/docs/troubleshooting"
	}
	return "https://docs.docker.com/engine/daemon/troubleshoot/"
}
