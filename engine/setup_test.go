package engine

import (
	"strings"
	"testing"
)

func TestLinuxRecommendsRootlessPodman(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}
	plans := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04"})
	if len(plans) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(plans))
	}
	if !plans[0].Recommended || plans[0].Runtime != "podman" {
		t.Fatalf("recommended plan = %#v, want Podman", plans[0])
	}
	if len(plans[0].Commands) != 2 || plans[0].Commands[1].Name != "sudo" {
		t.Fatalf("unexpected Ubuntu commands: %#v", plans[0].Commands)
	}
	if !strings.Contains(plans[0].Commands[1].Display, "apt-get install -y podman") {
		t.Fatalf("install command = %q", plans[0].Commands[1].Display)
	}
	if len(plans[0].Steps) != 3 || !strings.Contains(plans[0].PermissionNote, "account password") {
		t.Fatalf("Linux walkthrough is incomplete: %#v", plans[0])
	}
	if strings.Contains(strings.ToLower(plans[0].PermissionNote), "elevat") {
		t.Fatalf("password explanation uses unexplained technical language: %q", plans[0].PermissionNote)
	}
}

func TestOldUbuntuDoesNotRunAnUnsupportedPackageInstall(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}
	plan := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "20.04"})[0]
	if len(plan.Commands) != 0 || !plan.Manual {
		t.Fatalf("old Ubuntu should use official manual guidance: %#v", plan)
	}
}

func TestWindowsRecommendsDockerDesktop(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}
	plans := BuildSetupPlans(probes, HostPlatform{OS: "windows"})
	if plans[0].Recommended {
		t.Fatal("Podman should be an alternative on a fresh Windows setup")
	}
	if !plans[1].Recommended || plans[1].Runtime != "docker" {
		t.Fatalf("recommended plan = %#v, want Docker", plans[1])
	}
	if !strings.Contains(plans[1].URL, "ms-windows-store://") || !strings.Contains(plans[1].URL, "XP8CBJ40XLBWKX") {
		t.Fatalf("Windows Docker setup should open its official Store listing, URL = %q", plans[1].URL)
	}
	if len(plans[0].Steps) != 3 || !plans[0].DirectDownload || !strings.Contains(plans[0].URL, "podman-installer-windows-amd64.msi") {
		t.Fatalf("Podman alternative must have its own plain walkthrough: %#v", plans[0])
	}
	if len(plans[1].Steps) != 3 || !strings.Contains(plans[1].PermissionNote, "administrator") {
		t.Fatalf("Docker recommendation must explain its Windows setup: %#v", plans[1])
	}
}

func TestMacAlternativesHaveCompleteWalkthroughs(t *testing.T) {
	plans := BuildSetupPlans([]ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}, HostPlatform{OS: "darwin"})

	for _, plan := range plans {
		if len(plan.Steps) != 3 {
			t.Fatalf("%s walkthrough has %d steps, want 3: %#v", plan.Runtime, len(plan.Steps), plan)
		}
		if !strings.Contains(plan.PermissionNote, "Mac password") {
			t.Fatalf("%s walkthrough must explain a possible password request: %#v", plan.Runtime, plan)
		}
	}
}

func TestAppleSiliconMacDownloadsTheOfficialPodmanInstaller(t *testing.T) {
	plans := BuildSetupPlans([]ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}, HostPlatform{OS: "darwin", Arch: "arm64"})

	if !plans[0].Recommended || !plans[0].DirectDownload || !strings.HasSuffix(plans[0].URL, "podman-installer-macos-arm64.pkg") {
		t.Fatalf("unexpected Apple silicon Podman plan: %#v", plans[0])
	}
}

func TestIntelMacRecommendsDocker(t *testing.T) {
	plans := BuildSetupPlans([]ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}, HostPlatform{OS: "darwin", Arch: "amd64"})

	if plans[0].Recommended || !plans[1].Recommended {
		t.Fatalf("Intel Mac should recommend Docker: %#v", plans)
	}
	if !strings.Contains(plans[0].SafetyNote, "Intel Macs") {
		t.Fatalf("Intel Podman alternative must explain its limitation: %#v", plans[0])
	}
}

func TestWSLRecommendsDockerDesktop(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeMissing},
	}
	plans := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu", WSL: true})
	if !plans[1].Recommended || !strings.Contains(plans[1].Description, "Docker Desktop") {
		t.Fatalf("expected Docker Desktop recommendation under WSL, got %#v", plans[1])
	}
	if !strings.Contains(plans[1].URL, "ms-windows-store://") {
		t.Fatalf("WSL Docker setup should open Microsoft Store, URL = %q", plans[1].URL)
	}
}

func TestWindowsARMUsesDockerInstallerInsteadOfX64StoreListing(t *testing.T) {
	url := dockerInstallURL(HostPlatform{OS: "windows", Arch: "arm64"})
	if !strings.Contains(url, "windows-install") {
		t.Fatalf("Windows ARM URL = %q, want Docker's installer page", url)
	}
}

func TestStartDockerDesktopChecksPerUserAndAllUserLocations(t *testing.T) {
	command := startDockerDesktopCommand()
	args := strings.Join(command.Args, " ")
	if !strings.Contains(args, `LOCALAPPDATA\Programs\DockerDesktop`) {
		t.Fatalf("start command does not check the per-user Docker location: %s", args)
	}
	if !strings.Contains(args, `ProgramFiles\Docker\Docker`) {
		t.Fatalf("start command does not check the all-users Docker location: %s", args)
	}
}

func TestInstalledPodmanMachineRecoveryBeatsPlatformDefault(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMachineStopped},
		{Name: "docker", State: RuntimeMissing},
	}
	plans := BuildSetupPlans(probes, HostPlatform{OS: "windows"})
	if !plans[0].Recommended || plans[0].Commands[0].Display != "podman machine start" {
		t.Fatalf("expected Podman recovery recommendation, got %#v", plans[0])
	}
}

func TestPodmanMachineSetupDoesNotAskForADefaultConnection(t *testing.T) {
	plans := BuildSetupPlans([]ProbeResult{
		{Name: "podman", State: RuntimeMachineMissing},
		{Name: "docker", State: RuntimeMissing},
	}, HostPlatform{OS: "darwin"})

	if got := plans[0].Commands[0].Display; got != "podman machine init --now --update-connection=true" {
		t.Fatalf("machine setup command = %q", got)
	}
}

func TestDockerPermissionPlanDoesNotChangeGroups(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimePermissionDenied},
	}
	plans := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu"})
	dockerPlan := plans[1]
	if len(dockerPlan.Commands) != 0 {
		t.Fatalf("permission remediation must not run commands: %#v", dockerPlan.Commands)
	}
	if !strings.Contains(dockerPlan.SafetyNote, "full control") {
		t.Fatalf("missing group security explanation: %q", dockerPlan.SafetyNote)
	}
}

func TestStoppedDockerUsesSystemdOnlyWhenDetected(t *testing.T) {
	probes := []ProbeResult{
		{Name: "podman", State: RuntimeMissing},
		{Name: "docker", State: RuntimeStopped},
	}
	withSystemd := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu", Systemd: true})[1]
	if len(withSystemd.Commands) != 1 || withSystemd.Commands[0].Display != "sudo systemctl start docker" {
		t.Fatalf("unexpected systemd recovery: %#v", withSystemd)
	}
	withoutSystemd := BuildSetupPlans(probes, HostPlatform{OS: "linux", DistroID: "ubuntu"})[1]
	if len(withoutSystemd.Commands) != 0 || !withoutSystemd.Manual {
		t.Fatalf("non-systemd hosts must not run systemctl: %#v", withoutSystemd)
	}
}

func TestDockerURLMapsUbuntuDerivatives(t *testing.T) {
	url := dockerInstallURL(HostPlatform{OS: "linux", DistroID: "pop"})
	if url != "https://docs.docker.com/engine/install/ubuntu/" {
		t.Fatalf("URL = %q", url)
	}
}

func TestEverySupportedRuntimeStateHasACompleteNextStep(t *testing.T) {
	tests := []struct {
		name    string
		host    HostPlatform
		runtime string
		state   RuntimeState
	}{
		{"Windows Docker missing", HostPlatform{OS: "windows", Arch: "amd64"}, "docker", RuntimeMissing},
		{"Windows Docker stopped", HostPlatform{OS: "windows", Arch: "amd64"}, "docker", RuntimeStopped},
		{"Windows Docker permission", HostPlatform{OS: "windows", Arch: "amd64"}, "docker", RuntimePermissionDenied},
		{"Windows Docker broken", HostPlatform{OS: "windows", Arch: "amd64"}, "docker", RuntimeBroken},
		{"Windows Podman missing", HostPlatform{OS: "windows", Arch: "amd64"}, "podman", RuntimeMissing},
		{"Windows Podman machine missing", HostPlatform{OS: "windows", Arch: "amd64"}, "podman", RuntimeMachineMissing},
		{"Windows Podman machine stopped", HostPlatform{OS: "windows", Arch: "amd64"}, "podman", RuntimeMachineStopped},
		{"Windows Podman permission", HostPlatform{OS: "windows", Arch: "amd64"}, "podman", RuntimePermissionDenied},
		{"Windows Podman broken", HostPlatform{OS: "windows", Arch: "amd64"}, "podman", RuntimeBroken},
		{"Mac Docker missing", HostPlatform{OS: "darwin", Arch: "arm64"}, "docker", RuntimeMissing},
		{"Mac Docker stopped", HostPlatform{OS: "darwin", Arch: "arm64"}, "docker", RuntimeStopped},
		{"Mac Docker permission", HostPlatform{OS: "darwin", Arch: "arm64"}, "docker", RuntimePermissionDenied},
		{"Mac Docker broken", HostPlatform{OS: "darwin", Arch: "arm64"}, "docker", RuntimeBroken},
		{"Mac Podman missing", HostPlatform{OS: "darwin", Arch: "arm64"}, "podman", RuntimeMissing},
		{"Mac Podman machine missing", HostPlatform{OS: "darwin", Arch: "arm64"}, "podman", RuntimeMachineMissing},
		{"Mac Podman machine stopped", HostPlatform{OS: "darwin", Arch: "arm64"}, "podman", RuntimeMachineStopped},
		{"Mac Podman permission", HostPlatform{OS: "darwin", Arch: "arm64"}, "podman", RuntimePermissionDenied},
		{"Mac Podman broken", HostPlatform{OS: "darwin", Arch: "arm64"}, "podman", RuntimeBroken},
		{"Linux Docker missing", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker", RuntimeMissing},
		{"Linux Docker stopped", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker", RuntimeStopped},
		{"Linux Docker permission", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker", RuntimePermissionDenied},
		{"Linux Docker old", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker", RuntimeUnsupportedVersion},
		{"Linux Docker broken", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker", RuntimeBroken},
		{"Linux Podman missing", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "podman", RuntimeMissing},
		{"Linux Podman stopped", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "podman", RuntimeStopped},
		{"Linux Podman permission", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "podman", RuntimePermissionDenied},
		{"Linux Podman broken", HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "podman", RuntimeBroken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := "docker"
			if tt.runtime == "docker" {
				other = "podman"
			}
			plans := BuildSetupPlans([]ProbeResult{
				{Name: tt.runtime, State: tt.state},
				{Name: other, State: RuntimeReady},
			}, tt.host)
			if len(plans) != 1 {
				t.Fatalf("plan count = %d, want 1: %#v", len(plans), plans)
			}
			plan := plans[0]
			if plan.Runtime != tt.runtime || plan.State != tt.state {
				t.Fatalf("plan identity = %s/%s, want %s/%s", plan.Runtime, plan.State, tt.runtime, tt.state)
			}
			if plan.Title == "" || plan.Action == "" || plan.Description == "" || len(plan.Steps) == 0 {
				t.Fatalf("user walkthrough is incomplete: %#v", plan)
			}
			if len(plan.Commands) == 0 && plan.URL == "" {
				t.Fatalf("plan has no safe action: %#v", plan)
			}
			for _, command := range plan.Commands {
				if command.Name == "" || command.Display == "" {
					t.Fatalf("command is incomplete: %#v", command)
				}
			}
			if plan.URL != "" && !strings.Contains(plan.URL, "://") {
				t.Fatalf("URL is not actionable: %q", plan.URL)
			}
		})
	}
}

func TestWindowsPodmanInstallerMatchesOfficialReleaseAssets(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		plans := BuildSetupPlans([]ProbeResult{{Name: "podman", State: RuntimeMissing}}, HostPlatform{OS: "windows", Arch: arch})
		want := "podman-installer-windows-" + arch + ".msi"
		if len(plans) != 1 || !strings.HasSuffix(plans[0].URL, want) {
			t.Fatalf("%s installer = %#v, want suffix %q", arch, plans, want)
		}
	}
}
