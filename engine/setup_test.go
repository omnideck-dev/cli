package engine

import (
	"strconv"
	"strings"
	"testing"
)

func TestSetupPlansIgnoreDockerProbes(t *testing.T) {
	plans := BuildSetupPlans([]ProbeResult{
		{Name: "docker", State: RuntimeReady},
		{Name: "podman", State: RuntimeMissing},
	}, HostPlatform{OS: "windows", Arch: "amd64"})
	if len(plans) != 1 || plans[0].Title != "Podman" || !plans[0].Recommended {
		t.Fatalf("plans = %#v, want one recommended Podman plan", plans)
	}
}

func TestLinuxPodmanInstallUsesKnownPackageManager(t *testing.T) {
	plans := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMissing}},
		HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04"},
	)
	if len(plans) != 1 || len(plans[0].Commands) != 2 {
		t.Fatalf("plans = %#v", plans)
	}
	if got := plans[0].Commands[1].Display; got != "sudo apt-get install -y podman" {
		t.Fatalf("install command = %q", got)
	}
	if !plans[0].RequiresElevation || !strings.Contains(plans[0].PermissionNote, "account password") {
		t.Fatalf("permission guidance = %#v", plans[0])
	}
}

func TestUnknownOrOldLinuxUsesManualPodmanGuidance(t *testing.T) {
	for _, host := range []HostPlatform{
		{OS: "linux", DistroID: "unknown"},
		{OS: "linux", DistroID: "ubuntu", Version: "20.04"},
	} {
		plan := BuildSetupPlans([]ProbeResult{{Name: "podman", State: RuntimeMissing}}, host)[0]
		if !plan.Manual || len(plan.Commands) != 0 || !strings.Contains(plan.URL, "podman.io") {
			t.Fatalf("manual plan = %#v", plan)
		}
	}
}

func TestLinuxDerivativesUseTheirDeclaredPackageFamily(t *testing.T) {
	tests := []struct {
		host    HostPlatform
		manager string
	}{
		{HostPlatform{OS: "linux", DistroID: "kali", DistroLike: []string{"debian"}, Version: "2026.1"}, "apt-get"},
		{HostPlatform{OS: "linux", DistroID: "nobara", DistroLike: []string{"fedora"}}, "dnf"},
		{HostPlatform{OS: "linux", DistroID: "garuda", DistroLike: []string{"arch"}}, "pacman"},
	}
	for _, tt := range tests {
		commands := podmanLinuxPackageCommands(tt.host)
		if len(commands) == 0 || commands[0].Name != tt.manager {
			t.Fatalf("commands for %#v = %#v, want %s", tt.host, commands, tt.manager)
		}
	}
}

func TestFedoraPodmanInstallChecksPackagesBeforeInstalling(t *testing.T) {
	commands := podmanLinuxPackageCommands(HostPlatform{OS: "linux", DistroID: "fedora"})
	if len(commands) != 2 || commands[0].Display != "dnf makecache --refresh" || commands[1].Display != "dnf install -y podman" {
		t.Fatalf("Fedora commands = %#v", commands)
	}
}

func TestWindowsPodmanInstallerAndWSLGuidance(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		plan := BuildSetupPlans(
			[]ProbeResult{{Name: "podman", State: RuntimeMissing}},
			HostPlatform{OS: "windows", Arch: arch},
		)[0]
		if !plan.DirectDownload || !strings.HasSuffix(plan.URL, "podman-installer-windows-"+arch+".msi") {
			t.Fatalf("%s plan = %#v", arch, plan)
		}
		if !strings.Contains(plan.Description, "WSL 2") || !strings.Contains(plan.SafetyNote, "Hyper-V") {
			t.Fatalf("Windows guidance = %#v", plan)
		}
	}
}

func TestMacPodmanInstallPaths(t *testing.T) {
	arm := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMissing}},
		HostPlatform{OS: "darwin", Arch: "arm64"},
	)[0]
	if !arm.DirectDownload || !strings.HasSuffix(arm.URL, "podman-installer-macos-arm64.pkg") {
		t.Fatalf("Apple Silicon plan = %#v", arm)
	}
	intel := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMissing}},
		HostPlatform{OS: "darwin", Arch: "amd64"},
	)[0]
	if !intel.DirectDownload || !strings.HasSuffix(intel.URL, "podman-installer-macos-amd64.pkg") || strings.Contains(intel.Description, "Docker") {
		t.Fatalf("Intel plan = %#v", intel)
	}
}

func TestFreshWindowsMachineUsesSharedOmnideckDefaults(t *testing.T) {
	plan := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMachineMissing}},
		HostPlatform{OS: "windows", Arch: "amd64", CPUCount: 16, TotalMemoryMB: 32768},
	)[0]
	want := "podman machine init --provider wsl --user-mode-networking=true --rootful=false --now --tls-verify=true --update-connection=false omnideck-runtime"
	if got := plan.Commands[0].Display; got != want {
		t.Fatalf("machine init = %q, want %q", got, want)
	}
	if !strings.Contains(plan.Steps[0], "WSL 2") || !strings.Contains(plan.PermissionNote, "administrator") {
		t.Fatalf("machine guidance = %#v", plan)
	}
}

func TestFreshMacMachineSetsOnlyCompatibleMemoryAndSharedName(t *testing.T) {
	tests := []struct {
		name   string
		host   HostPlatform
		memory string
	}{
		{"small", HostPlatform{OS: "darwin", CPUCount: 1, TotalMemoryMB: 8192}, "4096"},
		{"medium", HostPlatform{OS: "darwin", CPUCount: 8, TotalMemoryMB: 12288}, "5120"},
		{"large", HostPlatform{OS: "darwin", CPUCount: 12, TotalMemoryMB: 32768}, "6144"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := BuildSetupPlans([]ProbeResult{{Name: "podman", State: RuntimeMachineMissing}}, tt.host)[0].Commands[0].Display
			for _, want := range []string{"--memory " + tt.memory, "--update-connection=false", OmnideckMachineName} {
				if !strings.Contains(command, want) {
					t.Fatalf("command %q does not contain %q", command, want)
				}
			}
			for _, omitted := range []string{"--cpus", "--disk-size", "--provider", "--user-mode-networking"} {
				if strings.Contains(command, omitted) {
					t.Fatalf("macOS command should leave %s to Podman: %q", omitted, command)
				}
			}
		})
	}
}

func TestMacContainerDefaultsFitInsideMachineMemory(t *testing.T) {
	tests := []struct {
		totalMB       int64
		container     string
		machineMemory int64
	}{
		{4096, "1g", 4096},
		{8192, "2g", 4096},
		{12288, "3g", 5120},
		{32768, "4g", 6144},
		{65536, "4g", 6144},
	}
	for _, tt := range tests {
		got := DefaultRuntimeResources(HostPlatform{OS: "darwin", TotalMemoryMB: tt.totalMB})
		if got.ContainerMemory != tt.container || got.MachineMemoryMB != tt.machineMemory {
			t.Fatalf("host memory %d MB defaults = %#v", tt.totalMB, got)
		}
		containerGB, _ := strconv.ParseInt(strings.TrimSuffix(got.ContainerMemory, "g"), 10, 64)
		if got.MachineMemoryMB < containerGB*1024+2048 {
			t.Fatalf("machine memory %d MB does not leave 2 GiB above container limit %s", got.MachineMemoryMB, got.ContainerMemory)
		}
	}
}

func TestWindowsContainerDefaultsFitInsideWSLDefaults(t *testing.T) {
	// Without a user .wslconfig, WSL exposes up to half of host memory. Keep at
	// least one GiB for the WSL guest above the container's cgroup limit; the
	// Windows host retains the other half independently.
	for _, totalMB := range []int64{4096, 8192, 16384, 32768, 65536} {
		got := DefaultRuntimeResources(HostPlatform{OS: "windows", TotalMemoryMB: totalMB})
		containerGB, _ := strconv.ParseInt(strings.TrimSuffix(got.ContainerMemory, "g"), 10, 64)
		wslDefaultMB := totalMB / 2
		if containerGB*1024+1024 > wslDefaultMB {
			t.Fatalf("host memory %d MB gives WSL about %d MB but container limit %s leaves less than 1 GiB guest headroom", totalMB, wslDefaultMB, got.ContainerMemory)
		}
		if got.MachineCPUs != 0 || got.MachineMemoryMB != 0 || got.MachineDiskGB != 0 {
			t.Fatalf("Windows must leave machine sizing to WSL, got %#v", got)
		}
	}
}

func TestRuntimeResourceDefaultsAreExplicitPerPlatform(t *testing.T) {
	tests := []struct {
		name         string
		host         HostPlatform
		mode         string
		memory       string
		shm          string
		machineCPUs  int
		machineMemMB int64
		machineDisk  int
	}{
		{
			name: "Windows uses WSL sizing",
			host: HostPlatform{OS: "windows", CPUCount: 16, TotalMemoryMB: 32 * 1024},
			mode: "wsl-managed", memory: "4g", shm: "2048m",
		},
		{
			name: "large Mac keeps VM headroom",
			host: HostPlatform{OS: "darwin", CPUCount: 12, TotalMemoryMB: 64 * 1024},
			mode: "podman-managed", memory: "4g", shm: "2048m",
			machineMemMB: 6144,
		},
		{
			name: "Linux uses the host directly",
			host: HostPlatform{OS: "linux", CPUCount: 8, TotalMemoryMB: 16 * 1024},
			mode: "host-native", memory: "3g", shm: "1536m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultRuntimeResources(tt.host)
			if got.MachineMode != tt.mode || got.ContainerMemory != tt.memory || got.ContainerSHMSize != tt.shm ||
				got.MachineCPUs != tt.machineCPUs || got.MachineMemoryMB != tt.machineMemMB || got.MachineDiskGB != tt.machineDisk {
				t.Fatalf("defaults = %#v", got)
			}
		})
	}
}

func TestStoppedMachineStartsTheSharedOmnideckMachine(t *testing.T) {
	plan := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMachineStopped, MachineName: "developer-machine"}},
		HostPlatform{OS: "windows"},
	)[0]
	if got := plan.Commands[0].Display; got != "podman machine start omnideck-runtime" {
		t.Fatalf("start command = %q; it must target the shared Omnideck machine", got)
	}
}

func TestWindowsNetworkingMigrationOnlyChangesTheSharedMachine(t *testing.T) {
	plan := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMachineNeedsUpdate, MachineName: "developer-machine", MachineRunning: true}},
		HostPlatform{OS: "windows"},
	)[0]
	want := []string{
		"podman machine stop omnideck-runtime",
		"podman machine set --user-mode-networking=true --rootful=false omnideck-runtime",
		"podman machine start omnideck-runtime",
	}
	if len(plan.Commands) != len(want) {
		t.Fatalf("commands = %#v, want %d commands", plan.Commands, len(want))
	}
	for index, display := range want {
		if plan.Commands[index].Display != display {
			t.Fatalf("command %d = %q, want %q", index, plan.Commands[index].Display, display)
		}
	}
}

func TestStoppedWindowsNetworkingMigrationDoesNotStopMachineAgain(t *testing.T) {
	plan := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeMachineNeedsUpdate, MachineRunning: false}},
		HostPlatform{OS: "windows"},
	)[0]
	if len(plan.Commands) != 2 || strings.Contains(plan.Commands[0].Display, " machine stop ") {
		t.Fatalf("commands = %#v", plan.Commands)
	}
}

func TestBrokenMachineRepairIsPlatformSpecific(t *testing.T) {
	for _, goos := range []string{"windows", "darwin"} {
		plan := BuildSetupPlans(
			[]ProbeResult{{Name: "podman", State: RuntimeBroken}},
			HostPlatform{OS: goos},
		)[0]
		if len(plan.Commands) != 2 || plan.Manual {
			t.Fatalf("%s plan = %#v", goos, plan)
		}
		for _, command := range plan.Commands {
			if !strings.HasSuffix(command.Display, " omnideck-runtime") {
				t.Fatalf("%s repair targets another machine: %q", goos, command.Display)
			}
		}
	}

	linuxPlan := BuildSetupPlans(
		[]ProbeResult{{Name: "podman", State: RuntimeBroken}},
		HostPlatform{OS: "linux"},
	)[0]
	if !linuxPlan.Manual || len(linuxPlan.Commands) != 0 {
		t.Fatalf("Linux must not run Podman machine commands: %#v", linuxPlan)
	}
}

func TestEveryPodmanStateHasACompleteNextStep(t *testing.T) {
	tests := []struct {
		host  HostPlatform
		state RuntimeState
	}{
		{HostPlatform{OS: "windows", Arch: "amd64"}, RuntimeMissing},
		{HostPlatform{OS: "windows"}, RuntimeMachineMissing},
		{HostPlatform{OS: "windows"}, RuntimeMachineStopped},
		{HostPlatform{OS: "windows"}, RuntimeMachineNeedsUpdate},
		{HostPlatform{OS: "windows"}, RuntimePermissionDenied},
		{HostPlatform{OS: "windows"}, RuntimeBroken},
		{HostPlatform{OS: "darwin", Arch: "arm64"}, RuntimeMissing},
		{HostPlatform{OS: "darwin", Arch: "amd64"}, RuntimeMissing},
		{HostPlatform{OS: "darwin"}, RuntimeMachineMissing},
		{HostPlatform{OS: "darwin"}, RuntimeMachineStopped},
		{HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04"}, RuntimeMissing},
		{HostPlatform{OS: "linux"}, RuntimeStopped},
		{HostPlatform{OS: "linux"}, RuntimePermissionDenied},
		{HostPlatform{OS: "linux"}, RuntimeBroken},
	}
	for _, tt := range tests {
		plan := BuildSetupPlans([]ProbeResult{{Name: "podman", State: tt.state}}, tt.host)[0]
		if plan.Title == "" || plan.Action == "" || plan.Description == "" || len(plan.Steps) == 0 {
			t.Fatalf("incomplete plan for %s/%s: %#v", tt.host.OS, tt.state, plan)
		}
		if len(plan.Commands) == 0 && plan.URL == "" {
			t.Fatalf("plan has no safe action: %#v", plan)
		}
	}
}

func TestVersionHelpers(t *testing.T) {
	if !versionAtLeast("24.04", 20, 10) || versionAtLeast("20.04", 20, 10) {
		t.Fatal("versionAtLeast comparison is incorrect")
	}
}
