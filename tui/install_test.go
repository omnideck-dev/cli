package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/engine"
)

func TestValidMemSize(t *testing.T) {
	valid := []string{"256m", "512M", "1g", "1G", "128k", "128K"}
	invalid := []string{"256", "256mb", "abc", "", "1.5g"}

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

func TestNewInstallModelDefaults(t *testing.T) {
	m := NewInstallModel("/tmp/test-config.yaml", nil, "")
	if m.Phase != PhasePreflight {
		t.Errorf("initial phase should be PhasePreflight, got %d", m.Phase)
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
	m := NewInstallModel("/tmp/test-config.yaml", nil, "")
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
	m := NewInstallModel("/tmp/test-config.yaml", nil, "my-custom-image:latest")
	cfg := m.buildConfig()
	if cfg.Image != "my-custom-image:latest" {
		t.Errorf("Image: got %q, want 'my-custom-image:latest'", cfg.Image)
	}
}

func TestValidateInputEmpty(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputs[inputContainerName].SetValue("")
	if m.validateCurrentInput() {
		t.Error("empty container name should fail validation")
	}
	if m.inputErrs[inputContainerName] == "" {
		t.Error("validation error should be set")
	}
}

func TestValidateInputShmSizeBad(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256mb") // invalid
	if m.validateCurrentInput() {
		t.Error("'256mb' should fail shm size validation")
	}
}

func TestValidateInputShmSizeGood(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256m")
	if !m.validateCurrentInput() {
		t.Error("'256m' should pass validation")
	}
}

func TestValidateInputMemoryBad(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("2gb") // invalid
	if m.validateCurrentInput() {
		t.Error("'2gb' should fail memory validation")
	}
}

func TestValidateInputMemoryGood(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
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

func TestPreflightAdvancement(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	// Simulate all checks arriving.
	m.preflightDone = 3 // one before 4
	cmd := m.maybeAdvancePreflight()
	if cmd != nil {
		t.Error("should not advance until 4 checks complete")
	}
	m.preflightDone = 4
	cmd = m.maybeAdvancePreflight()
	if cmd == nil {
		t.Error("should advance after 4 checks complete")
	}
}

func TestUpdatePreflightEngineOK(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	// When eng == nil, both the engine check and the skipped permission check
	// are counted immediately, so preflightDone goes from 0 to 2.
	msg := engineCheckResult{eng: nil, err: nil}
	m.preflightDone = 0
	newModel, _ := m.updatePreflight(msg)
	nm := newModel.(InstallModel)
	if nm.preflightDone != 2 {
		t.Errorf("preflightDone should be 2, got %d", nm.preflightDone)
	}
}

func TestUpdatePreflightEngineError(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.preflightDone = 4 // all other checks done
	msg := engineCheckResult{eng: nil, err: fmt.Errorf("no engine")}
	newModel, _ := m.updatePreflight(msg)
	nm := newModel.(InstallModel)
	// allPreflightDone will be fired next tick, but we test direct path:
	// Set preflightDone high and fire allPreflightDone directly.
	nm.engErr = msg.err
	nm2, _ := nm.updatePreflight(allPreflightDone{})
	final := nm2.(InstallModel)
	if final.Phase != PhaseRuntimeSetup {
		t.Errorf("should enter runtime setup phase when engine missing, got %d", final.Phase)
	}
}

func TestSetupOnlyReturnsToDashboardWhenRuntimeIsReady(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.Embedded = true
	m.setupOnly = true
	m.eng = &mockEngine{}

	newModel, cmd := m.afterRuntimeReady()
	nm := newModel.(InstallModel)
	if nm.Phase == PhaseConfig {
		t.Fatal("runtime repair must not continue into new-instance settings")
	}
	if cmd == nil {
		t.Fatal("runtime repair should return to the dashboard")
	}
	if _, ok := cmd().(WizardExitMsg); !ok {
		t.Fatal("runtime repair should emit WizardExitMsg")
	}
}

func TestContainerNameCollisionIsRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewInstallModel("/tmp/test.yaml", nil, "")
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
	m := NewInstallModel("/tmp/test.yaml", nil, "")
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
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	docker := &mockEngine{name: "docker"}
	podman := &mockEngine{name: "podman"}
	m.preflightReady = true
	m.preferredEngine = "docker"
	m.eng = docker
	m.availableEngines = []engine.Engine{docker, podman}

	newModel, cmd := m.updatePreflight(tea.KeyMsg{Type: tea.KeyTab})
	nm := newModel.(InstallModel)
	if cmd != nil || nm.eng.Name() != "docker" {
		t.Fatalf("per-instance switch changed runtime to %s", nm.eng.Name())
	}
}

func TestFreshSetupCanChooseMissingPodmanWhenDockerIsReady(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	docker := &mockEngine{name: "docker"}
	m.preflightReady = true
	m.eng = docker
	m.availableEngines = []engine.Engine{docker}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeReady},
	}

	if view := m.tnPreflight(100); !strings.Contains(view, "Set up Podman instead") {
		t.Fatalf("fresh setup does not show the Podman choice:\n%s", view)
	}

	newModel, cmd := m.updatePreflight(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatal("choosing the alternative should not run anything")
	}
	nm := newModel.(InstallModel)
	if nm.preflightAlternative != "podman" {
		t.Fatalf("preflight alternative = %q, want podman", nm.preflightAlternative)
	}

	newModel, cmd = nm.updatePreflight(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening Podman setup should not install anything immediately")
	}
	nm = newModel.(InstallModel)
	if nm.Phase != PhaseRuntimeSetup || nm.preferredEngine != "podman" || len(nm.runtimePlans) != 1 || nm.runtimePlans[0].Runtime != "podman" {
		t.Fatalf("Podman setup was not selected safely: phase=%d preferred=%q plans=%#v", nm.Phase, nm.preferredEngine, nm.runtimePlans)
	}

	newModel, _ = nm.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	nm = newModel.(InstallModel)
	if nm.Phase != PhasePreflight || nm.preferredEngine != "" {
		t.Fatalf("back did not return to the runtime choice: phase=%d preferred=%q", nm.Phase, nm.preferredEngine)
	}
}

func TestRecommendedNameSkipsUnrelatedContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.eng = &mockEngine{containerNames: map[string]bool{"omnideck": true}}
	m.ensureRecommendedSettingsAvailable()
	if got := m.inputs[inputContainerName].Value(); got != "omnideck2" {
		t.Fatalf("suggested name = %q, want omnideck2", got)
	}
}

func TestRuntimeSetupExplainsWhyAndRecommendsPlatformRuntime(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	view := m.viewRuntimeSetup()
	if !strings.Contains(view, "Choose Podman or Docker") || !strings.Contains(view, "isolated from the rest of your system") {
		t.Fatalf("runtime setup must explain why the dependency exists:\n%s", view)
	}
	if len(m.runtimePlans) != 2 {
		t.Fatalf("runtime plan count = %d, want 2", len(m.runtimePlans))
	}
}

func TestMissingContainerRuntimeDoesNotClaimAccountAccessWasChecked(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.preflightDone = 2
	m.engErr = fmt.Errorf("Podman and Docker are not ready")

	view := m.tnPreflight(100)
	if strings.Contains(view, "Account access") {
		t.Fatalf("account access cannot be checked before Podman or Docker is ready:\n%s", view)
	}
}

func TestRuntimeSetupReviewsBeforeRunningAnything(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	newModel, cmd := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(InstallModel)
	if cmd != nil {
		t.Fatal("the first Enter must only open the review; it must not run setup")
	}
	if !nm.runtimeConfirm {
		t.Fatal("the first Enter should open the setup review")
	}
	view := nm.tnRuntimeSetup(100)
	if !strings.Contains(view, "What happens next") || !strings.Contains(view, "Permission or password") {
		t.Fatalf("review must explain steps and password requests:\n%s", view)
	}
}

func TestRuntimeSetupReviewWrapsToAvailableWidth(t *testing.T) {
	const width = 56
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.Phase = PhaseRuntimeSetup
	m.runtimeConfirm = true
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

func TestRuntimeTechnicalDetailsHiddenUntilRequested(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeMissing},
		{Name: "docker", State: engine.RuntimeMissing},
	}
	m.configureRuntimeSetup()

	plan := m.runtimePlans[m.runtimeChoice]
	detail := plan.URL
	if len(plan.Commands) > 0 {
		detail = plan.Commands[0].Display
	}
	if detail == "" {
		t.Fatalf("selected setup plan has no command or URL: %#v", plan)
	}

	if view := m.tnRuntimeSetup(100); strings.Contains(view, "Technical details") || strings.Contains(view, detail) {
		t.Fatalf("technical details should be hidden by default:\n%s", view)
	}
	newModel, _ := m.updateRuntimeSetup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(InstallModel)
	if view := nm.tnRuntimeSetup(100); !strings.Contains(view, "Technical details") || !strings.Contains(strings.Join(strings.Fields(view), ""), strings.Join(strings.Fields(detail), "")) {
		t.Fatalf("technical details should show the selected plan's command or URL when requested:\n%s", view)
	}
}

func TestRecommendedSettingsAreOneStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.Phase = PhaseConfig

	view := m.tnConfig(100)
	if !strings.Contains(view, "Recommended settings are ready") || strings.Contains(view, "Shared memory") {
		t.Fatalf("simple settings view should hide advanced fields:\n%s", view)
	}
	newModel, _ := m.updateConfig(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(InstallModel)
	if nm.Phase != PhaseConfirm {
		t.Fatalf("Enter should accept recommended settings, phase = %d", nm.Phase)
	}
}

func TestSettingsCanBeCustomized(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.Phase = PhaseConfig
	newModel, _ := m.updateConfig(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	nm := newModel.(InstallModel)
	if !nm.configAdvanced {
		t.Fatal("c should open advanced settings")
	}
	if view := nm.tnConfig(100); !strings.Contains(view, "Shared memory") {
		t.Fatalf("advanced settings should explain every field:\n%s", view)
	}
}

func TestInstallErrorOffersRetryAndHidesTechnicalDetails(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.Phase = PhaseError
	m.errorMsg = "Download Omnideck"
	m.errorDetail = "connection reset by peer"

	view := m.tnError(100)
	if !strings.Contains(view, "Press r") || strings.Contains(view, m.errorDetail) {
		t.Fatalf("error screen must offer a retry and hide technical details by default:\n%s", view)
	}

	newModel, _ := m.updateError(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(InstallModel)
	if view := nm.tnError(100); !strings.Contains(view, m.errorDetail) {
		t.Fatalf("details should be available when requested:\n%s", view)
	}
}
