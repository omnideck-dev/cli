package tui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
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
	if m.Stage != SetupStageWelcome {
		t.Errorf("a first run should open on the welcome screen, got %d", m.Stage)
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

func TestValidateInputShmSizeCannotExceedContainerMemory(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.inputs[inputMemory].SetValue("1g")
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("2g")
	if m.validateCurrentInput() {
		t.Fatal("shared memory larger than the container limit should fail validation")
	}
	if !strings.Contains(m.inputErrs[inputShmSize], "must not be larger") {
		t.Fatalf("shared memory error = %q", m.inputErrs[inputShmSize])
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

func TestValidateInputMemoryCannotExceedDetectedSafeMaximum(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", TotalMemoryMB: 8192}
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("4g")
	if m.validateCurrentInput() {
		t.Fatal("memory above the detected safe maximum should fail validation")
	}
	if !strings.Contains(m.inputErrs[inputMemory], "maximum for this computer is 2g") {
		t.Fatalf("memory error = %q", m.inputErrs[inputMemory])
	}
}

func TestValidateInputMemoryGood(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64", TotalMemoryMB: 32 * 1024}
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
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
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

func TestContainerNameCollisionReturnsToSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageApplying
	m.spinnerModel = NewSpinnerModel(setupStepLabels, nil)
	err := checkSetupAvailability(&mockEngine{containerExists: true}, m.buildConfig())

	newModel, _ := m.updateApplying(StepFailedMsg{Index: setupStepAvailability, Err: err})
	nm := newModel.(SetupModel)
	if nm.Stage != SetupStageSettings || nm.inputFocus != inputContainerName {
		t.Fatalf("name collision stage = %d, focus = %d; want settings name field", nm.Stage, nm.inputFocus)
	}
	if !strings.Contains(nm.inputErrs[inputContainerName], "another container") {
		t.Fatalf("collision error = %q", nm.inputErrs[inputContainerName])
	}
}

func TestRuntimeFailureDuringAvailabilityShowsReportableError(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageApplying
	m.spinnerModel = NewSpinnerModel(setupStepLabels, nil)
	runtimeErr := errors.New("podman ps: exit status 125\nError: machine connection failed")

	newModel, _ := m.updateApplying(StepFailedMsg{Index: setupStepAvailability, Err: runtimeErr})
	nm := newModel.(SetupModel)
	if nm.Stage != SetupStageFailed || !nm.errorShowDetails {
		t.Fatalf("runtime failure stage = %d, details = %v; want visible failure", nm.Stage, nm.errorShowDetails)
	}
	view := nm.tnFailed(100)
	for _, want := range []string{"Check that the name", "podman ps", "machine connection failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("runtime failure view is missing %q:\n%s", want, view)
		}
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
	podman := &mockEngine{name: "podman"}
	m.quickCheckReady = true
	m.eng = podman
	m.availableEngines = []engine.Engine{podman}

	newModel, cmd := m.updateQuickCheck(tea.KeyMsg{Type: tea.KeyTab})
	nm := newModel.(SetupModel)
	if cmd != nil || nm.eng.Name() != "podman" {
		t.Fatalf("per-instance switch changed runtime to %s", nm.eng.Name())
	}
}

func TestFreshSetupUsesOnlyInstalledRuntimeWithoutOfferingMissingAlternative(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	m := NewSetupModel(SetupRequest{})
	podman := &mockEngine{name: "podman"}
	m.eng = podman
	m.availableEngines = []engine.Engine{podman}
	m.runtimeProbes = []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeReady},
	}

	newModel, cmd := m.updateQuickCheck(allQuickCheckDone{})
	nm := newModel.(SetupModel)
	if nm.Stage != SetupStageApplying || nm.eng.Name() != "podman" || nm.quickCheckReady || cmd == nil {
		t.Fatalf("single installed runtime did not continue automatically: stage=%d engine=%v ready=%t", nm.Stage, nm.eng, nm.quickCheckReady)
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

func TestRuntimeRepairDoesNotFallBackToReadyDocker(t *testing.T) {
	m := NewSetupModel(SetupRequest{Mode: SetupRuntimeRepair})
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

	if m.eng != nil || m.engErr == nil {
		t.Fatalf("ready Docker was accepted: engine %v error %v", m.eng, m.engErr)
	}
	if permissionCheck != nil {
		t.Fatal("ready Docker must not trigger an account-access check")
	}
}

func TestMissingContainerRuntimeDoesNotClaimAccountAccessWasChecked(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.quickCheckDone = 2
	m.engErr = fmt.Errorf("Podman is not ready")

	view := m.tnQuickCheck(100)
	if strings.Contains(view, "Account access") {
		t.Fatalf("account access cannot be checked before Podman is ready:\n%s", view)
	}
}

func TestFirstRunStartsFromDesktopWelcome(t *testing.T) {
	m := NewSetupModel(SetupRequest{Mode: SetupFirstRun})
	if m.Stage != SetupStageWelcome {
		t.Fatalf("first-run stage = %d, want welcome", m.Stage)
	}
	view := m.TUIView(88, 28)
	for _, want := range []string{
		"Welcome to omnideck",
		"A one-time setup will prepare everything omnideck needs on this computer.",
		"Press Enter to set up omnideck.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome is missing %q:\n%s", want, view)
		}
	}
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SetupModel)
	if m.Stage != SetupStageQuickCheck || cmd == nil {
		t.Fatalf("Enter = stage %d command %v, want automatic detection", m.Stage, cmd)
	}
	working := m.tnQuickCheck(88)
	for _, want := range []string{"Getting your computer ready…", "Computer setup", "Application files", "Final checks"} {
		if !strings.Contains(working, want) {
			t.Fatalf("first-run working screen is missing %q:\n%s", want, working)
		}
	}
	for _, internal := range []string{"Account access", "Available memory", "This computer"} {
		if strings.Contains(working, internal) {
			t.Fatalf("first-run working screen exposes %q:\n%s", internal, working)
		}
	}
}

func TestAutomaticRuntimeSetupStreamsDesktopPhasesAndContinues(t *testing.T) {
	original := ensureRuntimeForTUI
	t.Cleanup(func() { ensureRuntimeForTUI = original })
	terminalElevation := false
	ensureRuntimeForTUI = func(options engine.RuntimeSetupOptions) (engine.ProbeResult, error) {
		terminalElevation = options.AllowTerminalElevation
		softwareProgress := 0.5
		for _, event := range []engine.RuntimeSetupEvent{
			{Stage: engine.SetupStageSoftware, State: "start", Activity: engine.SetupActivitySoftware},
			{Stage: engine.SetupStageSoftware, State: "progress", Activity: engine.SetupActivitySoftware, Progress: &softwareProgress},
			{Stage: engine.SetupStageSoftware, State: "done", Activity: engine.SetupActivitySoftware},
			{Stage: engine.SetupStageEnvironment, State: "start", Activity: engine.SetupActivityEnvironment},
			{Stage: engine.SetupStageEnvironment, State: "done", Activity: engine.SetupActivityEnvironment},
		} {
			options.OnEvent(event)
		}
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeReady}, nil
	}

	m := NewSetupModel(SetupRequest{Mode: SetupFirstRun, AutoStart: true})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64", TotalMemoryMB: 32 * 1024}
	resources := engine.DefaultRuntimeResources(m.hostPlatform)
	m.inputs[inputMemory].SetValue(resources.ContainerMemory)
	m.inputs[inputShmSize].SetValue(resources.ContainerSHMSize)
	m.configureRuntimeSetup()
	cmd := m.startRuntimeSetup()
	seen := map[string]bool{}
	for cmd != nil && m.Stage == SetupStageRuntime {
		msg := cmd()
		if event, ok := msg.(runtimeSetupEventMsg); ok {
			seen[event.event.Activity] = true
		}
		newModel, next := m.updateRuntimeSetup(msg)
		m = newModel.(SetupModel)
		cmd = next
	}
	if !seen[engine.SetupActivitySoftware] || !seen[engine.SetupActivityEnvironment] {
		t.Fatalf("shared phases seen = %#v", seen)
	}
	if !terminalElevation {
		t.Fatal("the interactive CLI must allow a terminal-native sudo prompt")
	}
	if m.Stage != SetupStageApplying || m.eng == nil || m.eng.Name() != "podman" || cmd == nil {
		t.Fatalf("automatic setup stopped at stage=%d engine=%v command=%v", m.Stage, m.eng, cmd)
	}
}

func TestAutomaticRuntimeSetupOffersRestartAndResume(t *testing.T) {
	original := ensureRuntimeForTUI
	t.Cleanup(func() { ensureRuntimeForTUI = original })
	ensureRuntimeForTUI = func(engine.RuntimeSetupOptions) (engine.ProbeResult, error) {
		return engine.ProbeResult{Name: "podman", State: engine.RuntimeMissing}, &engine.RuntimeSetupError{
			Failure: engine.RuntimeSetupRestart,
			Message: "Windows must restart to finish enabling WSL 2.",
			Hint:    "Restart Windows, then continue setup.",
		}
	}

	m := NewSetupModel(SetupRequest{Mode: SetupFirstRun, AutoStart: true})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.configureRuntimeSetup()
	msg := m.startRuntimeSetup()()
	newModel, _ := m.updateRuntimeSetup(msg)
	m = newModel.(SetupModel)
	view := m.tnRuntimeSetup(88)
	for _, want := range []string{"Restart needed", "Press Enter to restart now", "Press l to restart later", "continues setup"} {
		if !strings.Contains(view, want) {
			t.Fatalf("restart view is missing %q:\n%s", want, view)
		}
	}
}

func TestAutomaticRuntimeSetupHasNoManualInstallerDetour(t *testing.T) {
	m := NewSetupModel(SetupRequest{Mode: SetupFirstRun, AutoStart: true})
	m.hostPlatform = engine.HostPlatform{OS: "windows", Arch: "amd64"}
	m.configureRuntimeSetup()
	view := m.tnRuntimeSetup(88)
	for _, unwanted := range []string{"STEP 1 OF 2", "STEP 2 OF 2", ".msi", "Downloads", "Open the", "Choose one"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("automatic setup exposed %q:\n%s", unwanted, view)
		}
	}
}

func TestWindowsPodmanDoesNotClaimHostOnlyOllamaIsReady(t *testing.T) {
	m := NewSetupModel(SetupRequest{Mode: SetupAdditionalInstance})
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

func TestReadyPodmanCompletesTheInstanceSetupFlow(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	for index, runtimeName := range []string{"podman"} {
		t.Run(runtimeName, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			_ = listener.Close()

			m := NewSetupModel(SetupRequest{Mode: SetupAdditionalInstance})
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
			labels := []string{"Preparing your environment", "Downloading omnideck’s files…", "Computer setup", "Application files", "Final checks", setupStepLabels[0]}
			if m.hostPlatform.OS != "linux" {
				labels = append(labels, "Secure space")
			}
			for _, label := range labels {
				if !strings.Contains(working, label) {
					t.Fatalf("%s working screen is missing %q:\n%s", runtimeName, label, working)
				}
			}
			for step := range setupStepLabels {
				newModel, _ = m.updateApplying(StepDoneMsg{Index: step})
				m = newModel.(SetupModel)
			}
			if m.Stage != SetupStageComplete || !strings.Contains(m.tnComplete(88), "omnideck is ready") {
				t.Fatalf("%s did not reach the ready screen", runtimeName)
			}
		})
	}
}

func TestSetupApplyingConsumesSharedLifecycleTransaction(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()

	m := NewSetupModel(SetupRequest{Mode: SetupAdditionalInstance})
	m.eng = &mockEngine{name: "podman"}
	m.inputs[inputContainerName].SetValue("shared-lifecycle")
	m.inputs[inputWebUIPort].SetValue(port)
	m.Stage = SetupStageApplying
	m.spinnerModel = NewSpinnerModel(setupStepLabels, nil)

	msg := m.startSetupWorkflow()()
	for iterations := 0; iterations < 20 && m.Stage != SetupStageComplete && m.Stage != SetupStageFailed; iterations++ {
		updated, _ := m.updateApplying(msg)
		m = updated.(SetupModel)
		if m.Stage == SetupStageComplete || m.Stage == SetupStageFailed {
			break
		}
		msg = <-m.setupEvents
	}
	if m.Stage != SetupStageComplete {
		t.Fatalf("shared lifecycle ended at stage %d: %s", m.Stage, m.errorDetail)
	}
	if _, err := config.Load(config.InstancePath("shared-lifecycle")); err != nil {
		t.Fatalf("shared lifecycle did not save config: %v", err)
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

// runAllCmds executes cmd and, if it returns a tea.BatchMsg, recursively
// executes every batched command too — Bubble Tea's runtime does this
// dispatch for real programs, but a unit test driving tea.Batch's result
// directly has to do it itself.
func runAllCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			runAllCmds(c)
		}
	}
}

func TestSetupFailureAfterBothVolumesRollsBackBothVolumes(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageApplying
	m.spinnerModel = NewSpinnerModel(setupStepLabels, nil)
	m.lastCompletedStep = setupStepStateVolume
	m.homeVolumeCreated = true
	m.stateVolumeCreated = true
	eng := &mockEngine{}
	m.eng = eng
	cfg := m.buildConfig()

	_, cmd := m.updateApplying(StepFailedMsg{Index: setupStepImage, Err: errors.New("pull failed")})
	if cmd == nil {
		t.Fatal("expected a rollback command when volumes were already created")
	}
	runAllCmds(cmd)

	wantVolumes := map[string]bool{cfg.HomeVolumeName(): true, cfg.StateVolumeName(): true}
	if len(eng.removedVolumes) != 2 {
		t.Fatalf("removedVolumes = %v, want both volumes removed", eng.removedVolumes)
	}
	for _, v := range eng.removedVolumes {
		if !wantVolumes[v] {
			t.Fatalf("unexpected volume removed: %s", v)
		}
	}
	if eng.containerExists {
		t.Fatal("the container was never created, so rollback must not touch it")
	}
}

func TestSetupFailureBeforeAnyVolumeNeedsNoRollback(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageApplying
	m.spinnerModel = NewSpinnerModel(setupStepLabels, nil)
	m.lastCompletedStep = setupStepAvailability
	eng := &mockEngine{}
	m.eng = eng

	_, cmd := m.updateApplying(StepFailedMsg{Index: setupStepHomeVolume, Err: errors.New("disk full")})
	runAllCmds(cmd)

	if len(eng.removedVolumes) != 0 {
		t.Fatalf("removedVolumes = %v, want none — nothing was created yet", eng.removedVolumes)
	}
}

func TestInstallErrorOffersRetryAndShowsReportableDetails(t *testing.T) {
	m := NewSetupModel(SetupRequest{})
	m.Stage = SetupStageFailed
	m.errorMsg = "Download Omnideck"
	m.errorDetail = "connection reset by peer"
	m.errorShowDetails = true

	view := m.tnFailed(100)
	if !strings.Contains(view, "Press r") || !strings.Contains(view, "Details to share") || !strings.Contains(view, m.errorDetail) {
		t.Fatalf("error screen must offer a retry and show reportable details by default:\n%s", view)
	}

	newModel, _ := m.updateFailed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	nm := newModel.(SetupModel)
	if view := nm.tnFailed(100); strings.Contains(view, m.errorDetail) {
		t.Fatalf("details should be hidden when requested:\n%s", view)
	}
}
