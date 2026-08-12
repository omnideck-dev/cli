package engine

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/omnideck-dev/cli/checks"
)

// OmnideckMachineName is the Podman machine shared by the desktop and CLI on
// macOS and Windows. Both surfaces select it explicitly, leaving a developer's
// existing default Podman machine untouched.
const OmnideckMachineName = "omnideck-runtime"

// HostPlatform contains only the host facts used to choose a safe setup path.
type HostPlatform struct {
	OS            string
	Arch          string
	DistroID      string
	DistroLike    []string
	Version       string
	Variant       string
	WSL           bool
	Systemd       bool
	Immutable     bool
	CPUCount      int
	TotalMemoryMB int64
}

// RuntimeResourceDefaults is the single resource policy shared by the TUI and
// Desktop. Windows leaves VM sizing to WSL, Linux has no VM, and macOS sets
// only the memory ceiling needed by the container while leaving CPU and sparse
// disk defaults to Podman.
type RuntimeResourceDefaults struct {
	ContainerMemory  string
	ContainerSHMSize string
	MachineMode      string
	MachineCPUs      int
	MachineMemoryMB  int64
	MachineDiskGB    int
}

// DefaultRuntimeResources derives safe defaults from host capacity. The
// container is capped below the macOS VM limit so Podman and the guest OS keep
// headroom even on large Macs.
func DefaultRuntimeResources(host HostPlatform) RuntimeResourceDefaults {
	memory, shm := "2g", "1024m"
	if host.TotalMemoryMB > 0 {
		memory, shm = checks.DefaultContainerMemory(host.TotalMemoryMB)
	}
	defaults := RuntimeResourceDefaults{
		ContainerMemory:  memory,
		ContainerSHMSize: shm,
		MachineMode:      "host-native",
	}
	switch host.OS {
	case "windows":
		defaults.MachineMode = "wsl-managed"
	case "darwin":
		defaults.MachineMode = "podman-managed"
		if defaults.ContainerMemory == "6g" {
			defaults.ContainerMemory = "4g"
			defaults.ContainerSHMSize = "2048m"
		}
		defaults.MachineMemoryMB = macMachineMemoryMB(defaults.ContainerMemory)
	}
	return defaults
}

// macMachineMemoryMB keeps two GiB for Podman and the guest OS above the
// container's cgroup limit. Four GiB remains the minimum useful VM ceiling.
// Unlike WSL, a macOS Podman machine needs an explicit memory ceiling; CPU time
// and sparse disk growth can safely stay with Podman's platform defaults.
func macMachineMemoryMB(containerMemory string) int64 {
	machineMB := int64(4096)
	containerGB, err := strconv.ParseInt(strings.TrimSuffix(containerMemory, "g"), 10, 64)
	if err != nil || containerGB <= 0 {
		return machineMB
	}
	if requiredMB := containerGB*1024 + 2048; requiredMB > machineMB {
		machineMB = requiredMB
	}
	return machineMB
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
	host := HostPlatform{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCount: runtime.NumCPU()}
	if totalMB, err := checks.TotalMemoryMB(); err == nil {
		host.TotalMemoryMB = totalMB
	}
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
	host.DistroLike = strings.Fields(values["ID_LIKE"])
	host.Version = values["VERSION_ID"]
	host.Variant = values["VARIANT_ID"]
	host.Immutable = immutableLinuxHost(host)
	if _, err := os.Stat("/run/ostree-booted"); err == nil {
		host.Immutable = true
	}
	return host
}

func immutableLinuxVariant(variant string) bool {
	switch strings.ToLower(variant) {
	case "silverblue", "kinoite", "sericea", "onyx", "coreos":
		return true
	default:
		return false
	}
}

func immutableLinuxHost(host HostPlatform) bool {
	if immutableLinuxVariant(host.Variant) {
		return true
	}
	for _, id := range append([]string{host.DistroID}, host.DistroLike...) {
		switch strings.ToLower(id) {
		case "bazzite", "coreos", "fedora-coreos", "flatcar", "microos", "rhcos", "ublue-os":
			return true
		}
	}
	return false
}

func supportedHostTarget(host HostPlatform) bool {
	if host.Arch != "amd64" && host.Arch != "arm64" {
		return false
	}
	switch host.OS {
	case "linux", "darwin", "windows":
		return true
	default:
		return false
	}
}

func normalizeHostPlatform(host HostPlatform) HostPlatform {
	if host.OS == "" {
		host.OS = runtime.GOOS
	}
	if host.Arch == "" {
		host.Arch = runtime.GOARCH
	}
	return host
}

// BuildSetupPlans describes the Podman recovery or installation path for the
// current host. Unsupported legacy runtime probes are deliberately ignored.
func BuildSetupPlans(probes []ProbeResult, host HostPlatform) []SetupPlan {
	host = normalizeHostPlatform(host)
	plans := make([]SetupPlan, 0, len(probes))
	for _, probe := range probes {
		if probe.Name != "podman" || probe.Ready() {
			continue
		}
		plans = append(plans, explainSetupPlan(setupPlanFor(probe, host), probe, host))
	}

	if len(plans) == 0 {
		return plans
	}
	plans[0].Recommended = true
	if isSimpleRecovery(probeState(probes, "podman")) {
		plans[0].Recommendation = "Podman is already installed, so finishing its setup is the quickest option."
	} else {
		plans[0].Recommendation = freshRecommendation(host)
	}
	return plans
}

func freshRecommendation(host HostPlatform) string {
	if host.OS == "darwin" {
		return "Podman is a free option with an installer provided by the Podman project."
	}
	return "Podman is the container runtime Omnideck uses on every supported platform."
}

func explainSetupPlan(plan SetupPlan, probe ProbeResult, host HostPlatform) SetupPlan {
	plan.Action = "Set up " + plan.Title
	switch probe.State {
	case RuntimeMachineMissing:
		plan.Action = "Finish setting up Podman"
		plan.Description = "Podman is installed, but its one-time setup is not finished."
		plan.Steps = []string{
			"Prepare Podman's small Linux environment. Podman may download files it needs.",
			"Start Podman.",
			"Check that Omnideck can use it.",
		}
		if host.OS == "windows" {
			plan.Description = "Podman is installed. Omnideck can finish its one-time Windows setup."
			plan.Steps[0] = "Create Podman's small Linux environment using WSL 2, the recommended Windows option."
			plan.PermissionNote = "If Windows says WSL 2 must be turned on, it will ask for permission from an administrator and may need to restart the computer. Omnideck cannot see or store an administrator password."
			plan.SafetyNote = "WSL 2 is the easiest choice for most people and is Podman's default. Hyper-V is an advanced alternative for managed work computers; it requires Windows Pro or Enterprise and an administrator."
		}
	case RuntimeMachineStopped:
		plan.Action = "Start Podman"
		plan.Description = "Podman is installed and only needs to be started."
		plan.Steps = []string{"Start Podman.", "Check that Omnideck can use it."}
	case RuntimeMachineNeedsUpdate:
		plan.Action = "Update Podman's secure space"
		plan.Description = "Podman's existing Omnideck environment needs a one-time networking update."
		plan.Steps = []string{
			"Stop the Omnideck Podman environment if it is running.",
			"Turn on user-mode networking so containers can reliably reach Windows services.",
			"Start Podman and check that Omnideck can use it.",
		}
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
		plan.Action = "Update Podman"
		plan.Description = "Podman is installed, but this version is too old for Omnideck."
		plan.Steps = []string{"Open Podman's official installation page.", "Update Podman.", "Return to Omnideck and check again."}
	case RuntimeBroken:
		if len(plan.Commands) > 0 {
			plan.Action = "Restart Podman"
			plan.Description = "The Omnideck Podman environment is running, but its connection is not responding."
			plan.Steps = []string{"Stop the Omnideck Podman environment.", "Start it again.", "Check that Omnideck can use it."}
		} else {
			plan.Action = "Open help for " + plan.Title
			plan.Description = plan.Title + " is installed, but it is not working yet."
			plan.Steps = []string{"Open the official help page.", "Follow the steps for starting or repairing " + plan.Title + ".", "Return to Omnideck and check again."}
		}
	case RuntimeMissing:
		explainMissingPlan(&plan, host)
	}

	if plan.RequiresElevation {
		plan.PermissionNote = "Your computer may ask for your account password before installing Podman. The password gives your computer's built-in installer permission to add the app. Omnideck does not see or store it."
	}
	if host.OS == "darwin" && probe.ConflictingMachineName != "" && len(plan.Commands) > 0 {
		plan.Action = "Switch Podman to omnideck"
		plan.Description = "macOS can run only one Podman machine at a time. Another one is currently running."
		plan.Steps = []string{
			"Stop \"" + probe.ConflictingMachineName + "\". This keeps its files but stops any containers running inside it.",
			"Start the dedicated omnideck secure space.",
			"Check that omnideck can use it.",
		}
		plan.SafetyNote = "The other Podman machine is stopped, not removed. Its files, images, and containers stay on this Mac."
	}
	return plan
}

func explainMissingPlan(plan *SetupPlan, host HostPlatform) {
	plan.Action = "Install " + plan.Title
	if host.OS == "linux" && len(plan.Commands) > 0 {
		plan.Description = "Install Podman, the free option recommended for this computer."
		plan.Steps = []string{
			"Ask your computer for the latest list of available apps.",
			"Download and install Podman.",
			"Check that Omnideck can use it.",
		}
		return
	}
	if host.OS == "darwin" {
		plan.Description = "Download Podman's official Mac installer."
		installer, _ := podmanInstallerFor(host)
		plan.Steps = []string{
			"Wait for the official Podman installer download to finish.",
			"Open " + installer.Filename + " from Downloads and follow the instructions on screen.",
			"Return to Omnideck and check again.",
		}
		plan.DirectDownload = true
		plan.PermissionNote = "The Podman installer may ask for your Mac password so macOS can add the app. Omnideck does not see or store it."
		return
	}
	if host.OS == "windows" {
		installer, _ := podmanInstallerFor(host)
		plan.Description = "Download Podman's official Windows installer. WSL 2 is recommended for most people."
		plan.Steps = []string{
			"Wait for the official Podman installer download to finish.",
			"Open " + installer.Filename + " from Downloads and keep the recommended Just for me choice.",
			"Return to Omnideck and check again. Omnideck will finish setup using WSL 2, Podman's default Windows option.",
		}
		plan.DirectDownload = true
		plan.PermissionNote = "Installing Podman just for your account does not require an administrator. If Windows needs to turn on WSL 2, it will ask for permission from an administrator and may need to restart the computer."
		plan.SafetyNote = "Use WSL 2 unless the person who manages this computer specifically asks for Hyper-V. Hyper-V is an advanced option that requires Windows Pro or Enterprise and an administrator."
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
	return state == RuntimeStopped || state == RuntimeMachineMissing || state == RuntimeMachineStopped || state == RuntimeMachineNeedsUpdate
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
		State:       probe.State,
		Title:       "Podman",
		Description: RuntimeStateLabel(probe.State),
	}

	switch probe.State {
	case RuntimeMachineMissing:
		plan.Description = "Finish Podman's one-time setup"
		plan.Commands = append(macMachineConflictCommands(probe, host), podmanMachineInitCommand(host))
		return plan
	case RuntimeMachineStopped:
		plan.Description = "Start Podman"
		plan.Commands = append(macMachineConflictCommands(probe, host), command("podman", "machine", "start", OmnideckMachineName))
		return plan
	case RuntimeMachineNeedsUpdate:
		plan.Description = "Update Podman's networking"
		if probe.MachineRunning {
			plan.Commands = append(plan.Commands, command("podman", "machine", "stop", OmnideckMachineName))
		}
		plan.Commands = append(plan.Commands,
			command("podman", "machine", "set", "--user-mode-networking=true", "--rootful=false", OmnideckMachineName),
			command("podman", "machine", "start", OmnideckMachineName),
		)
		return plan
	case RuntimeStopped:
		return stoppedPlan(plan, host)
	case RuntimePermissionDenied:
		plan.Manual = true
		plan.Description = "Get help using Podman"
		plan.URL = podmanTroubleshootingURL
		return plan
	case RuntimeUnsupportedVersion:
		plan.Description = "Update Podman"
		plan.Manual = true
		plan.URL = "https://podman.io/docs/installation"
		return plan
	case RuntimeBroken:
		if podmanPolicy(host.OS).UsesMachine {
			plan.Description = "Restart Podman"
			plan.Commands = []SetupCommand{
				command("podman", "machine", "stop", OmnideckMachineName),
				command("podman", "machine", "start", OmnideckMachineName),
			}
			return plan
		}
		plan.Description = "Open official help for this app"
		plan.Manual = true
		plan.URL = podmanTroubleshootingURL
		return plan
	default:
		return missingPlan(plan, host)
	}
}

func macMachineConflictCommands(probe ProbeResult, host HostPlatform) []SetupCommand {
	name := probe.ConflictingMachineName
	if host.OS != "darwin" || name == "" || name == OmnideckMachineName {
		return nil
	}
	return []SetupCommand{command("podman", "machine", "stop", name)}
}

func podmanMachineInitCommand(host HostPlatform) SetupCommand {
	args := []string{"machine", "init"}
	if host.OS == "windows" {
		args = append(args,
			"--provider", "wsl",
			"--user-mode-networking=true",
		)
	} else if host.OS == "darwin" {
		resources := DefaultRuntimeResources(host)
		args = append(args,
			"--memory", strconv.FormatInt(resources.MachineMemoryMB, 10),
		)
	}
	args = append(args,
		"--rootful=false",
		"--now",
		"--tls-verify=true",
		"--update-connection=false",
		OmnideckMachineName,
	)
	return command("podman", args...)
}

func stoppedPlan(plan SetupPlan, host HostPlatform) SetupPlan {
	if podmanPolicy(host.OS).UsesMachine {
		plan.Description = "Start Podman"
		plan.Commands = []SetupCommand{command("podman", "machine", "start", OmnideckMachineName)}
		return plan
	}
	plan.Description = "Get help starting Podman"
	plan.Manual = true
	plan.URL = podmanTroubleshootingURL
	return plan
}

func missingPlan(plan SetupPlan, host HostPlatform) SetupPlan {
	if !supportedHostTarget(host) {
		plan.Description = "This operating system or architecture is not supported by this Omnideck release"
		plan.Manual = true
		plan.URL = "https://github.com/omnideck-dev/cli/releases"
		return plan
	}
	switch host.OS {
	case "linux":
		plan.Description = "Install Podman"
		plan.Commands, plan.RequiresElevation = podmanLinuxCommands(host)
		if len(plan.Commands) == 0 {
			plan.Manual = true
			plan.URL = "https://podman.io/docs/installation"
		}
	case "windows", "darwin":
		installer, ok := podmanInstallerFor(host)
		if !ok {
			plan.Description = "This release does not include Podman for this computer architecture"
			plan.Manual = true
			plan.URL = "https://github.com/omnideck-dev/cli/releases"
			return plan
		}
		plan.Description = "Install Podman with its official " + map[bool]string{true: "Windows", false: "Mac"}[host.OS == "windows"] + " installer"
		plan.URL = installer.URL()
		plan.DirectDownload = true
		plan.Manual = true
	}
	return plan
}

func podmanLinuxCommands(host HostPlatform) ([]SetupCommand, bool) {
	commands := podmanLinuxPackageCommands(host)
	for index, packageCommand := range commands {
		commands[index] = command("sudo", append([]string{packageCommand.Name}, packageCommand.Args...)...)
	}
	return commands, len(commands) > 0
}

func podmanLinuxPackageCommands(host HostPlatform) []SetupCommand {
	if host.Immutable {
		return nil
	}
	distroFamily := host.DistroID
	if !knownLinuxDistroFamily(distroFamily) {
		for _, candidate := range host.DistroLike {
			if knownLinuxDistroFamily(candidate) {
				distroFamily = candidate
				break
			}
		}
	}
	switch distroFamily {
	case "ubuntu":
		if host.DistroID == "ubuntu" && host.Version != "" && !versionAtLeast(host.Version, 20, 10) {
			return nil
		}
		return []SetupCommand{
			command("apt-get", "update"),
			command("apt-get", "install", "-y", "podman"),
		}
	case "debian":
		if host.DistroID == "debian" && host.Version != "" && !versionAtLeast(host.Version, 11, 0) {
			return nil
		}
		return []SetupCommand{
			command("apt-get", "update"),
			command("apt-get", "install", "-y", "podman"),
		}
	case "linuxmint", "pop":
		return []SetupCommand{
			command("apt-get", "update"),
			command("apt-get", "install", "-y", "podman"),
		}
	case "fedora", "rhel", "centos", "rocky", "almalinux", "amzn", "ol":
		return []SetupCommand{
			command("dnf", "makecache", "--refresh"),
			command("dnf", "install", "-y", "podman"),
		}
	case "arch", "manjaro", "endeavouros":
		return []SetupCommand{command("pacman", "-S", "--needed", "podman")}
	case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
		return []SetupCommand{command("zypper", "install", "-y", "podman")}
	case "alpine":
		return []SetupCommand{command("apk", "add", "podman")}
	default:
		return nil
	}
}

func knownLinuxDistroFamily(value string) bool {
	switch value {
	case "ubuntu", "debian", "linuxmint", "pop",
		"fedora", "rhel", "centos", "rocky", "almalinux", "amzn", "ol",
		"arch", "manjaro", "endeavouros",
		"opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles", "alpine":
		return true
	default:
		return false
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

const podmanTroubleshootingURL = "https://podman.io/docs/troubleshooting"
