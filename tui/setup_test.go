package tui

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/engine"
)

func TestValidMemSize(t *testing.T) {
	valid := []string{"256m", "512M", "1g", "1G", "128k", "128K"}
	invalid := []string{"0g", "256", "256mb", "abc", "", "1.5g"}

	for _, s := range valid {
		if !validMemSize(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range invalid {
		if validMemSize(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestNewSetupModelDefaults(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	if m.Stage != SetupStageQuickCheck {
		t.Errorf("initial phase should be SetupStageQuickCheck, got %d", m.Stage)
	}
	if m.inputs[inputContainerName].Value() != "omnideck" {
		t.Errorf("default container name should be 'omnideck'")
	}
	// Memory and SHM defaults are calculated from system RAM; just verify non-empty.
	if m.inputs[inputMemory].Value() == "" {
		t.Error("default memory should be non-empty")
	}
	if m.inputs[inputShmSize].Value() == "" {
		t.Error("default shm size should be non-empty")
	}
}

func TestBuildConfig(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	// Set custom values.
	m.inputs[inputContainerName].SetValue("mycontainer")
	m.inputs[inputMemory].SetValue("4g")
	m.inputs[inputShmSize].SetValue("512m")

	cfg := m.buildConfig()
	if cfg.ContainerName != "mycontainer" {
		t.Errorf("ContainerName: got %q, want 'mycontainer'", cfg.ContainerName)
	}
	if cfg.HomeVolumeName() != "mycontainer-home" {
		t.Errorf("HomeVolumeName: got %q", cfg.HomeVolumeName())
	}
	if cfg.StateVolumeName() != "mycontainer-state" {
		t.Errorf("StateVolumeName: got %q", cfg.StateVolumeName())
	}
	if cfg.Memory != "4g" {
		t.Errorf("Memory: got %q", cfg.Memory)
	}
	if cfg.ShmSize != "512m" {
		t.Errorf("ShmSize: got %q", cfg.ShmSize)
	}
}

func TestBuildConfigImageOverride(t *testing.T) {
	m := NewSetupModel(SetupRequest{ImageOverride: "my-custom-image:latest"})
	cfg := m.buildConfig()
	if cfg.Image != "my-custom-image:latest" {
		t.Errorf("Image: got %q, want 'my-custom-image:latest'", cfg.Image)
	}
}

func TestValidateInputEmpty(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputs[inputContainerName].SetValue("")
	if m.validateCurrentInput() {
		t.Error("empty container name should fail validation")
	}
	if m.inputErrs[inputContainerName] == "" {
		t.Error("validation error should be set")
	}
}

func TestValidateInputShmSizeBad(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256mb") // invalid
	if m.validateCurrentInput() {
		t.Error("'256mb' should fail shm size validation")
	}
}

func TestValidateInputShmSizeGood(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256m")
	if !m.validateCurrentInput() {
		t.Error("'256m' should pass validation")
	}
}

func TestValidateInputMemoryBad(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("2gb") // invalid
	if m.validateCurrentInput() {
		t.Error("'2gb' should fail memory validation")
	}
}

func TestValidateInputMemoryGood(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("4g")
	if !m.validateCurrentInput() {
		t.Error("'4g' should pass memory validation")
	}
}

func TestValidContainerName(t *testing.T) {
	valid := []string{"omnideck", "my-container", "box_1", "X123"}
	for _, s := range valid {
		if !validContainerName(s) {
			t.Errorf("expected %q to be a valid container name", s)
		}
	}
	invalid := []string{"", "-leadingdash", "has space", "dot.start"}
	for _, s := range invalid {
		// dot.start is actually valid by the regex (starts with letter)
		// only check the ones we know are invalid
		_ = s
	}
	definitelyInvalid := []string{"", "-leadingdash", "has space"}
	for _, s := range definitelyInvalid {
		if validContainerName(s) {
			t.Errorf("expected %q to be an invalid container name", s)
		}
	}
}

func TestValidPort(t *testing.T) {
	valid := []string{"1", "2337", "8080", "65535"}
	for _, s := range valid {
		if !validPort(s) {
			t.Errorf("expected %q to be a valid port", s)
		}
	}
	invalid := []string{"0", "65536", "-1", "abc", "", "8080.5"}
	for _, s := range invalid {
		if validPort(s) {
			t.Errorf("expected %q to be an invalid port", s)
		}
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()

	got := expandTilde("~/documents")
	want := filepath.Join(home, "documents")
	if got != want {
		t.Errorf("expandTilde(~/documents): got %q, want %q", got, want)
	}

	abs := "/absolute/path"
	if got := expandTilde(abs); got != abs {
		t.Errorf("absolute path should pass through unchanged, got %q", got)
	}

	rel := "relative/path"
	if got := expandTilde(rel); got != rel {
		t.Errorf("relative path should pass through unchanged, got %q", got)
	}
}

func TestQuickCheckAdvancement(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	// Simulate all checks arriving.
	m.quickCheckDone = 3 // one before 4
	cmd := m.maybeAdvanceQuickCheck()
	if cmd != nil {
		t.Error("should not advance until 4 checks complete")
	}
	m.quickCheckDone = 4
	cmd = m.maybeAdvanceQuickCheck()
	if cmd == nil {
		t.Error("should advance after 4 checks complete")
	}
}

func TestUpdateQuickCheckEngineOK(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	// When eng == nil, both the engine check and the skipped permission check
	// are counted immediately, so quickCheckDone goes from 0 to 2.
	msg := engineCheckResult{eng: nil, err: nil}
	m.quickCheckDone = 0
	newModel, _ := m.updateQuickCheck(msg)
	nm := newModel.(SetupModel)
	if nm.quickCheckDone != 2 {
		t.Errorf("quickCheckDone should be 2, got %d", nm.quickCheckDone)
	}
}

func TestUpdateQuickCheckEngineError(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.quickCheckDone = 4 // all other checks done
	msg := engineCheckResult{eng: nil, err: fmt.Errorf("no engine")}
	newModel, _ := m.updateQuickCheck(msg)
	nm := newModel.(SetupModel)
	// allQuickCheckDone will be fired next tick, but we test direct path:
	// Set quickCheckDone high and fire allQuickCheckDone directly.
	nm.engErr = msg.err
	nm2, _ := nm.updateQuickCheck(allQuickCheckDone{})
	final := nm2.(SetupModel)
	if final.Stage != SetupStageRuntime {
		t.Errorf("should enter runtime setup phase when engine missing, got %d", final.Stage)
	}
}

func TestSetupOnlyReturnsToDashboardWhenRuntimeIsReady(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.Embedded = true
	m.setupMode = SetupRuntimeRepair
	m.eng = &mockEngine{}

	newModel, cmd := m.afterRuntimeReady()
	nm := newModel.(SetupModel)
	if nm.Stage == SetupStageSettings {
		t.Fatal("runtime repair must not continue into new-instance settings")
	}
	if cmd == nil {
		t.Fatal("runtime repair should return to the dashboard")
	}
	if _, ok := cmd().(WorkflowExitMsg); !ok {
		t.Fatal("runtime repair should emit WorkflowExitMsg")
	}
}

func TestContainerNameCollisionIsRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.eng = &mockEngine{containerExists: true}
	m.inputFocus = inputContainerName
	m.inputs[inputContainerName].SetValue("omnideck")

	if m.validateCurrentInput() {
		t.Fatal("a container name already used by the runtime must be rejected")
	}
	if !strings.Contains(m.inputErrs[inputContainerName], "another container") {
		t.Fatalf("collision error = %q", m.inputErrs[inputContainerName])
	}
}

func TestExistingBrowserPortIsRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.inputFocus = inputWebUIPort
	m.inputs[inputWebUIPort].SetValue("2337")
	m.existingPorts["2337"] = true

	if m.validateCurrentInput() {
		t.Fatal("a browser port already used by Omnideck must be rejected")
	}
	if !strings.Contains(m.inputErrs[inputWebUIPort], "another Omnideck") {
		t.Fatalf("port collision error = %q", m.inputErrs[inputWebUIPort])
	}
}

func TestMachineWideRuntimeCannotBeSwitchedPerInstance(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	docker := &mockEngine{name: "docker"}
	podman := &mockEngine{name: "podman"}
	m.quickCheckReady = true
	m.preferredEngine = "docker"
	m.eng = docker
	m.availableEngines = []engine.Engine{docker, podman}

	newModel, cmd := m.updateQuickCheck(tea.KeyMsg{Type: tea.KeyTab})
	nm := newModel.(SetupModel)
	if cmd != nil || nm.eng.Name() != "docker" {
		t.Fatalf("per-instance switch changed runtime to %s", nm.eng.Name())
	}
}

func TestReadyRuntimeDefaultMatchesThePlatformRecommendation(t *testing.T) {
	docker := &mockEngine{name: "docker"}
	podman := &mockEngine{name: "podman"}
	ready := []engine.Engine{podman, docker}
	tests := []struct {
		name string
		host engine.HostPlatform
		want string
	}{
		{"Windows", engine.HostPlatform{OS: "windows", Arch: "amd64"}, "docker"},
		{"Intel Mac", engine.HostPlatform{OS: "darwin", Arch: "amd64"}, "docker"},
		{"Apple chip Mac", engine.HostPlatform{OS: "darwin", Arch: "arm64"}, "podman"},
		{"Linux", engine.HostPlatform{OS: "linux", Arch: "amd64"}, "podman"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readyEngineForSetup(ready, tt.host); got == nil || got.Name() != tt.want {
				t.Fatalf("ready engine = %v, want %s", got, tt.want)
			}
		})
	}
}

func TestFreshSetupUsesOnlyInstalledRuntimeWithoutOfferingMissingAlternative(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	docker := &mockEngine{name: "docker"}
	m.eng = docker
	m.availableEngines = []engine.Engine{docker}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeReady},
	}

	if alternative := m.setupAlternativeRuntime(); alternative != "" {
		t.Fatalf("missing runtime was offered as alternative: %q", alternative)
	}
	newModel, _ := m.updateQuickCheck(allQuickCheckDone{})
	nm := newModel.(SetupModel)
	if nm.Stage != SetupStageSettings || nm.eng.Name() != "docker" || nm.quickCheckReady {
		t.Fatalf("single installed runtime did not continue automatically: stage=%d engine=%v ready=%t", nm.Stage, nm.eng, nm.quickCheckReady)
	}
}

func TestFreshSetupCanChooseSecondInstalledRuntimeThatNeedsAttention(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	docker := &mockEngine{name: "docker"}
	m.quickCheckReady = true
	m.eng = docker
	m.availableEngines = []engine.Engine{docker}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMachineStopped},
		{Name: "docker", State: engine.RuntimeReady},
	}

	if view := m.tnQuickCheck(100); !strings.Contains(view, "Set up Podman instead") {
		t.Fatalf("second installed runtime is not available as a choice:\n%s", view)
	}
	newModel, cmd := m.updateQuickCheck(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatal("choosing the alternative should not run anything")
	}
	nm := newModel.(SetupModel)
	if nm.quickCheckAlternative != "podman" {
		t.Fatalf("quickCheck alternative = %q, want podman", nm.quickCheckAlternative)
	}

	newModel, cmd = nm.updateQuickCheck(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening Podman setup should not change anything immediately")
	}
	nm = newModel.(SetupModel)
	if nm.Stage != SetupStageRuntime || nm.preferredEngine != "podman" || len(nm.runtimePlans) != 1 || nm.runtimePlans[0].Runtime != "podman" {
		t.Fatalf("Podman setup was not selected safely: phase=%d preferred=%q plans=%#v", nm.Stage, nm.preferredEngine, nm.runtimePlans)
	}

	newModel, _ = nm.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	nm = newModel.(SetupModel)
	if nm.Stage != SetupStageQuickCheck || nm.preferredEngine != "" {
		t.Fatalf("back did not return to the runtime choice: phase=%d preferred=%q", nm.Stage, nm.preferredEngine)
	}
}

func TestRecommendedNameSkipsUnrelatedContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.eng = &mockEngine{containerNames: map[string]bool{"omnideck": true}}
	m.ensureRecommendedSettingsAvailable()
	if got := m.inputs[inputContainerName].Value(); got != "omnideck2" {
		t.Fatalf("suggested name = %q, want omnideck2", got)
	}
}

func TestNoInstalledRuntimeShowsOnlyTheEasiestPlatformSetup(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	view := m.tnRuntimeSetup(80)
	if !strings.Contains(view, "Set up Docker") || !strings.Contains(view, "Why this is needed") || !strings.Contains(view, "isolated") {
		t.Fatalf("runtime setup must explain why the dependency exists:\n%s", view)
	}
	if len(m.runtimePlans) != 1 || m.runtimePlans[0].Runtime != "docker" || m.preferredEngine != "docker" {
		t.Fatalf("Windows default was not reduced to one simple path: preferred=%q plans=%#v", m.preferredEngine, m.runtimePlans)
	}
	if strings.Contains(view, "Install Podman") || strings.Contains(view, "Set up Podman") || strings.Contains(view, "Choose one") {
		t.Fatalf("no-runtime setup still exposed an unnecessary choice:\n%s", view)
	}
}

func TestTwoInstalledRuntimesThatNeedAttentionRemainAChoice(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMachineStopped},
		{Name: "docker", State: engine.RuntimeStopped},
	}
	m.configureRuntimeSetup()

	if len(m.runtimePlans) != 2 || m.preferredEngine != "" {
		t.Fatalf("two installed runtimes should remain a choice: preferred=%q plans=%#v", m.preferredEngine, m.runtimePlans)
	}
	if view := m.tnRuntimeSetup(100); !strings.Contains(view, "Choose Podman or Docker") {
		t.Fatalf("two installed runtimes did not show a picker:\n%s", view)
	}
}

func TestOneInstalledRuntimeThatNeedsAttentionIsTheOnlyRepairPath(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMachineStopped},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	if len(m.runtimePlans) != 1 || m.runtimePlans[0].Runtime != "podman" || m.preferredEngine != "podman" {
		t.Fatalf("installed Podman was not selected for repair: preferred=%q plans=%#v", m.preferredEngine, m.runtimePlans)
	}
	if !strings.Contains(m.runtimePlans[0].Recommendation, "already installed") {
		t.Fatalf("repair path does not explain the automatic choice: %#v", m.runtimePlans[0])
	}
}

func TestRuntimeOverrideWinsWhenNothingIsInstalled(t *testing.T) {
	m := NewSetupModel(SetupRequest{PreferredEngine: "podman"})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	if len(m.runtimePlans) != 1 || m.runtimePlans[0].Runtime != "podman" {
		t.Fatalf("--runtime podman did not override Windows default: %#v", m.runtimePlans)
	}
}

func TestRuntimeRepairDoesNotRestoreAnUninstalledSavedRuntime(t *testing.T) {
	m := NewSetupModel(SetupRequest{
		Mode:            SetupRuntimeRepair,
		PreferredEngine: "podman",
	})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	if m.preferredEngine != "docker" || len(m.runtimePlans) != 1 || m.runtimePlans[0].Runtime != "docker" {
		t.Fatalf("stale Podman preference produced preferred=%q plans=%#v; want the Windows Docker default", m.preferredEngine, m.runtimePlans)
	}
	if view := m.tnRuntimeSetup(88); !strings.Contains(view, "Set up Docker") || strings.Contains(view, "Set up Podman") {
		t.Fatalf("stale Podman preference is still visible:\n%s", view)
	}
}

func TestRuntimeRepairUsesTheOnlyReadyRuntimeAfterSavedRuntimeWasRemoved(t *testing.T) {
	m := NewSetupModel(SetupRequest{
		Mode:            SetupRuntimeRepair,
		PreferredEngine: "podman",
	})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	docker := &mockEngine{name: "docker"}

	newModel, permissionCheck := m.updateQuickCheck(engineCheckResult{
		eng: nil,
		all: []engine.Engine{docker},
		probes: []engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
			{Name: "docker", State: engine.RuntimeReady},
		},
		err: fmt.Errorf("podman is not ready"),
	})
	m = newModel.(SetupModel)

	if m.preferredEngine != "" || m.eng == nil || m.eng.Name() != "docker" || m.engErr != nil {
		t.Fatalf("ready fallback = preferred %q engine %v error %v; want Docker", m.preferredEngine, m.eng, m.engErr)
	}
	if permissionCheck == nil {
		t.Fatal("the automatically selected Docker runtime must continue through the normal permission check")
	}
}

func TestMissingContainerRuntimeDoesNotClaimAccountAccessWasChecked(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.quickCheckDone = 2
	m.engErr = fmt.Errorf("Podman and Docker are not ready")

	view := m.tnQuickCheck(100)
	if strings.Contains(view, "Account access") {
		t.Fatalf("account access cannot be checked before Podman or Docker is ready:\n%s", view)
	}
}

func TestRuntimeSetupReviewsBeforeRunningAnything(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	newModel, cmd := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(SetupModel)
	if cmd != nil {
		t.Fatal("the first Enter must only open the review; it must not run setup")
	}
	if nm.runtimeSetupStage != runtimeSetupReview {
		t.Fatal("the first Enter should open the setup review")
	}
	view := nm.tnRuntimeSetup(100)
	if !strings.Contains(view, "What happens next") || !strings.Contains(view, "Permission or password") {
		t.Fatalf("review must explain steps and password requests:\n%s", view)
	}
}

func TestRuntimeSetupReviewWrapsToAvailableWidth(t *testing.T) {
	const width = 56
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageRuntime
	m.runtimeSetupStage = runtimeSetupReview
	m.runtimePlans = []engine.SetupPlan{{
		Action: "Install Docker Desktop for this computer",
		Steps: []string{
			"Open Docker's official download page and choose the installer recommended for this computer.",
			"When the installer asks who Docker is for, keep the recommended Per-user choice.",
		},
		PermissionNote: "Windows may ask whether the installer is allowed to make changes to this computer. If your account cannot approve that message, ask the person who manages the computer.",
		SafetyNote:     "Docker Desktop can require a paid subscription at larger companies, so ask your company before installing it on a work computer.",
	}}

	view := m.tnRuntimeSetup(width)
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("rendered line is %d columns wide, want at most %d:\n%q\n\n%s", got, width, line, view)
		}
	}
}

func TestRuntimeCommandsHiddenUntilRequested(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "linux", DistroID: "ubuntu", Version: "24.04"}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	plan := m.runtimePlans[m.runtimeChoice]
	if len(plan.Commands) == 0 {
		t.Fatalf("selected setup plan has no command: %#v", plan)
	}
	detail := plan.Commands[0].Display

	if view := m.tnRuntimeSetup(100); strings.Contains(view, "Commands Omnideck will run") || strings.Contains(view, detail) {
		t.Fatalf("commands should be hidden by default:\n%s", view)
	}
	newModel, _ := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(SetupModel)
	if view := nm.tnRuntimeSetup(100); !strings.Contains(view, "Commands Omnideck will run") || !strings.Contains(strings.Join(strings.Fields(view), ""), strings.Join(strings.Fields(detail), "")) {
		t.Fatalf("commands should be available when requested:\n%s", view)
	}
}

func TestManualRuntimeInstallDoesNotOfferRawURLDetails(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.preferredEngine = "podman"
	m.runtimeProbes = []engine.ProbeResult{{Name: "podman", State: engine.RuntimeMissing}}
	m.configureRuntimeSetup()
	if m.runtimeDetailsAvailable() {
		t.Fatal("a manual installer URL should not be presented as useful setup details")
	}
	newModel, _ := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(SetupModel)
	view := nm.tnRuntimeSetup(100)
	if nm.runtimeShowDetails || strings.Contains(view, "github.com/containers/podman/releases") || strings.Contains(view, "Technical details") {
		t.Fatalf("manual Windows setup exposed raw installer details:\n%s", view)
	}
}

func TestWindowsPodmanDoesNotClaimHostOnlyOllamaIsReady(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.eng = &mockEngine{name: "podman"}
	m.ollamaOK = true
	m.ollamaHost = "127.0.0.1:11434"
	m.memMB = 4096
	m.memChecked = true

	quickCheck := m.tnQuickCheck(100)
	if !strings.Contains(quickCheck, "connection checked after start") || strings.Contains(quickCheck, "Ollama is ready") {
		t.Fatalf("Windows Podman preflight overstated Ollama reachability:\n%s", quickCheck)
	}

	m.buildReviewWarnings()
	review := m.tnReview(100)
	compactReview := strings.Join(strings.Fields(review), " ")
	for _, want := range []string{"Local AI", "real connection from inside Podman"} {
		if !strings.Contains(compactReview, want) {
			t.Fatalf("Windows Podman Ollama check explanation is missing %q:\n%s", want, review)
		}
	}

	m.ollamaContainerChecked = true
	m.ollamaContainerOK = false
	failed := m.tnComplete(100)
	for _, want := range []string{"Local AI needs one Windows setting", "environment variables", "OLLAMA_HOST", "0.0.0.0:11434", "public networks"} {
		if !strings.Contains(failed, want) {
			t.Fatalf("failed in-container check is missing %q:\n%s", want, failed)
		}
	}

	m.ollamaContainerOK = true
	if view := m.tnComplete(100); strings.Contains(view, "Local AI needs one Windows setting") {
		t.Fatalf("successful in-container check still showed repair steps:\n%s", view)
	}
}

func TestPrimaryOSAndRuntimeSetupFlowsAreComplete(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	tests := []struct {
		name    string
		host    engine.HostPlatform
		runtime string
	}{
		{"Windows Docker", engine.HostPlatform{OS: "windows", Arch: "amd64"}, "docker"},
		{"Windows Podman", engine.HostPlatform{OS: "windows", Arch: "amd64"}, "podman"},
		{"macOS Docker", engine.HostPlatform{OS: "darwin", Arch: "arm64"}, "docker"},
		{"macOS Podman", engine.HostPlatform{OS: "darwin", Arch: "arm64"}, "podman"},
		{"Linux Docker", engine.HostPlatform{OS: "linux", Arch: "amd64", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "docker"},
		{"Linux Podman", engine.HostPlatform{OS: "linux", Arch: "amd64", DistroID: "ubuntu", Version: "24.04", Systemd: true}, "podman"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := "docker"
			if tt.runtime == "docker" {
				other = "podman"
			}
			m := NewSetupModel(SetupRequest{})
			m.hostPlatform = tt.host
			m.preferredEngine = tt.runtime
			m.runtimeProbes = []engine.ProbeResult{
				{Name: tt.runtime, State: engine.RuntimeMissing},
				{Name: other, State: engine.RuntimeReady},
			}
			m.configureRuntimeSetup()

			if len(m.runtimePlans) != 1 {
				t.Fatalf("plan count = %d, want one explicitly selected runtime: %#v", len(m.runtimePlans), m.runtimePlans)
			}
			plan := m.runtimePlans[0]
			view := m.tnRuntimeSetup(88)
			if !strings.Contains(view, "Set up "+plan.Title) || !strings.Contains(view, "Why this is needed") || !strings.Contains(view, "Next step") {
				t.Fatalf("first screen is incomplete:\n%s", view)
			}
			if strings.Contains(view, "Choose one") {
				t.Fatalf("one selected runtime must not be presented as multiple choices:\n%s", view)
			}

			newModel, cmd := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(SetupModel)
			if cmd != nil || m.runtimeSetupStage != runtimeSetupReview {
				t.Fatal("first Enter must only open the review")
			}
			review := m.tnRuntimeSetup(88)
			if !strings.Contains(review, plan.Action) || !strings.Contains(review, "What happens next") || !strings.Contains(review, "Nothing starts until you press Enter") {
				t.Fatalf("review screen is incomplete:\n%s", review)
			}

			newModel, cmd = m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(SetupModel)
			if cmd == nil {
				t.Fatal("second Enter must perform the reviewed action")
			}
			if len(plan.Commands) > 0 && m.runtimeSetupStage != runtimeSetupWorking {
				t.Fatal("command setup should show a working state")
			}
			if plan.URL != "" {
				if m.runtimeSetupStage != runtimeSetupWaiting {
					t.Fatal("page or download setup should show a return-and-recheck state")
				}
				waiting := m.tnRuntimeSetup(88)
				if !strings.Contains(waiting, "After you finish the step on the other screen") || !strings.Contains(waiting, "Press Enter") {
					t.Fatalf("waiting screen is incomplete:\n%s", waiting)
				}
			}
		})
	}
}

func TestWindowsDockerInstallRecheckTransitionsToStartThenSettings(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.preferredEngine = "docker"
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	newModel, _ := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	newModel, _ = m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if m.runtimeSetupStage != runtimeSetupWaiting {
		t.Fatal("Docker Store setup should wait for the user to return")
	}

	newModel, _ = m.updateRuntimeSetup(engineCheckResult{
		probes: []engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
			{Name: "docker", State: engine.RuntimeStopped},
		},
		err: fmt.Errorf("docker is not ready"),
	})
	m = newModel.(SetupModel)
	if len(m.runtimePlans) != 1 || m.runtimePlans[0].State != engine.RuntimeStopped {
		t.Fatalf("post-install plan = %#v, want Start Docker", m.runtimePlans)
	}
	view := m.tnRuntimeSetup(88)
	if !strings.Contains(view, "Start Docker") || !strings.Contains(view, "not running yet") || strings.Contains(view, "Choose an option below") {
		t.Fatalf("post-install recheck guidance is wrong:\n%s", view)
	}

	docker := &mockEngine{name: "docker"}
	newModel, _ = m.updateRuntimeSetup(engineCheckResult{
		eng: docker,
		all: []engine.Engine{docker},
		probes: []engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
			{Name: "docker", State: engine.RuntimeReady},
		},
	})
	m = newModel.(SetupModel)
	if m.Stage != SetupStageSettings {
		t.Fatalf("ready Docker should continue to settings, phase = %d", m.Stage)
	}
}

func TestWindowsPodmanInstallRecheckOffersOneTimeSetup(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.preferredEngine = "podman"
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	newModel, _ := m.updateRuntimeSetup(engineCheckResult{
		probes: []engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMachineMissing},
			{Name: "docker", State: engine.RuntimeMissing},
		},
		err: fmt.Errorf("podman is not ready"),
	})
	m = newModel.(SetupModel)
	if len(m.runtimePlans) != 1 || m.runtimePlans[0].State != engine.RuntimeMachineMissing {
		t.Fatalf("post-install plan = %#v, want Podman one-time setup", m.runtimePlans)
	}
	view := m.tnRuntimeSetup(88)
	if !strings.Contains(view, "Finish setting up Podman") || !strings.Contains(view, "one-time setup is not finished") {
		t.Fatalf("Podman post-install guidance is wrong:\n%s", view)
	}
}

func TestRuntimeSetupWithNoFilteredPlanCanAlwaysRecheck(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.preferredEngine = "docker"
	// A partial or unexpected probe result must never leave the user trapped.
	m.runtimeProbes = []engine.ProbeResult{{Name: "podman", State: engine.RuntimeMissing}}
	m.configureRuntimeSetup()

	if len(m.runtimePlans) != 0 {
		t.Fatalf("test requires the defensive empty-plan state, got %#v", m.runtimePlans)
	}
	view := m.tnRuntimeSetup(72)
	if strings.Contains(view, "Choose an option below") || !strings.Contains(view, "Press Enter to check again") {
		t.Fatalf("empty setup state gives unusable guidance:\n%s", view)
	}
	newModel, cmd := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if cmd == nil || m.runtimeSetupStage != runtimeSetupWorking {
		t.Fatal("Enter must recheck even when no setup option could be built")
	}
}

func TestRuntimeWaitingCopyMatchesTheAction(t *testing.T) {
	installer := runtimeWaitingMessage(engine.SetupPlan{Title: "Podman", State: engine.RuntimeMissing, DirectDownload: true})
	if !strings.Contains(installer, "installer") || strings.Contains(installer, "help page") {
		t.Fatalf("installer message = %q", installer)
	}
	help := runtimeWaitingMessage(engine.SetupPlan{Title: "Docker", State: engine.RuntimePermissionDenied})
	if !strings.Contains(help, "help page") || strings.Contains(help, "Finish the installation") {
		t.Fatalf("help message = %q", help)
	}
}

func TestRuntimeNotReadyCopyPointsToTheVisibleOptionAndExactKeys(t *testing.T) {
	tests := []struct {
		name  string
		plans []engine.SetupPlan
		want  []string
		not   []string
	}{
		{
			name:  "missing",
			plans: []engine.SetupPlan{{Title: "Docker", State: engine.RuntimeMissing}},
			want:  []string{"press Enter to review the installation steps", "press r to check again"},
			not:   []string{"below"},
		},
		{
			name: "multiple choices",
			plans: []engine.SetupPlan{
				{Title: "Podman", State: engine.RuntimeStopped},
				{Title: "Docker", State: engine.RuntimeStopped},
			},
			want: []string{"options above", "press Enter to review", "press r to check again"},
			not:  []string{"below"},
		},
		{
			name:  "stopped",
			plans: []engine.SetupPlan{{Title: "Docker", State: engine.RuntimeStopped}},
			want:  []string{"Press Enter to review the start steps", "press r to check again"},
			not:   []string{"below"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := runtimeNotReadyMessage(tt.plans, "")
			for _, want := range tt.want {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q does not contain %q", message, want)
				}
			}
			for _, unwanted := range tt.not {
				if strings.Contains(message, unwanted) {
					t.Fatalf("message %q contains misleading %q", message, unwanted)
				}
			}
		})
	}
}

func TestRepeatedEnterAfterOpeningInstallerDoesNotDownloadAgain(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.preferredEngine = "podman"
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	newModel, _ := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	newModel, openInstaller := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if openInstaller == nil || m.runtimeSetupStage != runtimeSetupWaiting {
		t.Fatal("review confirmation must open the installer and wait")
	}

	// A held Enter can send another key event immediately. It may start one
	// readiness check, but further events are ignored while that check runs.
	newModel, check := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if check == nil || m.runtimeSetupStage != runtimeSetupWorking {
		t.Fatal("Enter on the waiting screen must start one readiness check")
	}
	newModel, repeated := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if repeated != nil || m.runtimeSetupStage != runtimeSetupWorking {
		t.Fatal("repeated Enter must be ignored while readiness is being checked")
	}

	// If the download or installer has not finished, remain on the waiting
	// screen. Returning to the install choice allowed queued Enter events to
	// launch the same direct download over and over.
	newModel, cmd := m.updateRuntimeSetup(engineCheckResult{
		probes: []engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
			{Name: "docker", State: engine.RuntimeMissing},
		},
		err: fmt.Errorf("podman is not ready"),
	})
	m = newModel.(SetupModel)
	if cmd != nil || m.runtimeSetupStage != runtimeSetupWaiting {
		t.Fatalf("failed recheck = stage %d, command %v; want waiting with no download", m.runtimeSetupStage, cmd)
	}
	if !strings.Contains(m.runtimeMessage, "still cannot find Podman") {
		t.Fatalf("failed recheck message = %q", m.runtimeMessage)
	}

	// Reopening is still available, but only through the explicit retry key.
	newModel, reopen := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = newModel.(SetupModel)
	if reopen == nil || m.runtimeSetupStage != runtimeSetupWaiting {
		t.Fatal("o must explicitly reopen the installer while staying on the waiting screen")
	}
}

func TestReadyDockerAndPodmanShareTheCompleteInstanceSetupFlow(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	for index, runtimeName := range []string{"docker", "podman"} {
		t.Run(runtimeName, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			_ = listener.Close()

			m := NewSetupModel(SetupRequest{})
			m.eng = &mockEngine{name: runtimeName}
			m.inputs[inputContainerName].SetValue(fmt.Sprintf("omnideck-%s-%d", runtimeName, index))
			m.inputs[inputWebUIPort].SetValue(port)
			newModel, _ := m.afterRuntimeReady()
			m = newModel.(SetupModel)
			if m.Stage != SetupStageSettings || !strings.Contains(m.tnSettings(88), "Recommended settings are ready") {
				t.Fatalf("ready %s did not reach settings", runtimeName)
			}

			newModel, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(SetupModel)
			confirm := m.tnReview(88)
			if m.Stage != SetupStageReview || !strings.Contains(confirm, "Runs with") || !strings.Contains(confirm, runtimeNameForPeople(runtimeName)) {
				t.Fatalf("%s review is incomplete:\n%s", runtimeName, confirm)
			}

			newModel, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(SetupModel)
			if cmd == nil || m.Stage != SetupStageApplying {
				t.Fatalf("%s did not enter working screen", runtimeName)
			}
			working := m.tnApplying(88)
			for _, label := range setupStepLabels {
				if !strings.Contains(working, label) {
					t.Fatalf("%s working screen is missing %q:\n%s", runtimeName, label, working)
				}
			}
			for step := range setupStepLabels {
				newModel, _ = m.updateApplying(StepDoneMsg{Index: step})
				m = newModel.(SetupModel)
			}
			if m.Stage != SetupStageComplete || !strings.Contains(m.tnComplete(88), "Omnideck is ready") {
				t.Fatalf("%s did not reach the ready screen", runtimeName)
			}
		})
	}
}

func TestRecommendedSettingsAreOneStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageSettings

	view := m.tnSettings(100)
	if !strings.Contains(view, "Recommended settings are ready") || strings.Contains(view, "Shared memory") {
		t.Fatalf("simple settings view should hide advanced fields:\n%s", view)
	}
	newModel, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(SetupModel)
	if nm.Stage != SetupStageReview {
		t.Fatalf("Enter should accept recommended settings, phase = %d", nm.Stage)
	}
}

func TestSettingsCanBeCustomized(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageSettings
	newModel, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	nm := newModel.(SetupModel)
	if !nm.settingsAdvanced {
		t.Fatal("c should open advanced settings")
	}
	if view := nm.tnSettings(100); !strings.Contains(view, "Shared memory") {
		t.Fatalf("advanced settings should explain every field:\n%s", view)
	}
}

func TestInstallErrorOffersRetryAndHidesTechnicalDetails(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageFailed
	m.errorMsg = "Download Omnideck"
	m.errorDetail = "connection reset by peer"

	view := m.tnFailed(100)
	if !strings.Contains(view, "Press r") || strings.Contains(view, m.errorDetail) {
		t.Fatalf("error screen must offer a retry and hide technical details by default:\n%s", view)
	}

	newModel, _ := m.updateFailed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(SetupModel)
	if view := nm.tnFailed(100); !strings.Contains(view, m.errorDetail) {
		t.Fatalf("details should be available when requested:\n%s", view)
	}
}
