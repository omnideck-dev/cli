package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

func TestRenderDoctorReportAllPass(t *testing.T) {
	results := []CheckResult{
		{Label: "Engine", Status: CheckPass, Detail: "Podman 6.0"},
		{Label: "Memory", Status: CheckPass, Detail: "8000 MB"},
	}
	report, allPass := RenderDoctorReport(results)
	if !allPass {
		t.Error("expected allPass=true")
	}
	if !strings.Contains(report, "Engine") {
		t.Error("report should contain label")
	}
}

func TestRenderDoctorReportWithFail(t *testing.T) {
	results := []CheckResult{
		{Label: "Engine", Status: CheckFail, Detail: "not found", Hint: "Install Podman"},
		{Label: "Memory", Status: CheckPass, Detail: "8000 MB"},
	}
	_, allPass := RenderDoctorReport(results)
	if allPass {
		t.Error("expected allPass=false when any check fails")
	}
}

func TestRenderDoctorReportWarnIsNotFail(t *testing.T) {
	results := []CheckResult{
		{Label: "Ollama", Status: CheckWarn, Detail: "not reachable", Hint: "Install"},
		{Label: "Memory", Status: CheckPass, Detail: "8000 MB"},
	}
	_, allPass := RenderDoctorReport(results)
	if !allPass {
		t.Error("warnings alone should not fail the report")
	}
}

func TestRenderDoctorReportShowsHint(t *testing.T) {
	results := []CheckResult{
		{Label: "Container", Status: CheckFail, Detail: "not found", Hint: "Run: omnideck setup"},
	}
	report, _ := RenderDoctorReport(results)
	if !strings.Contains(report, "omnideck setup") {
		t.Error("hint should appear in report")
	}
}

func TestVolumeCheckMissing(t *testing.T) {
	r := workflow.CheckVolume("Test volume", "missing-volume", &mockEngine{})
	if r.Status != CheckFail {
		t.Error("missing volume should be CheckFail")
	}
}

func TestVolumeCheckExists(t *testing.T) {
	r := workflow.CheckVolume("Test volume", "omnideck-home", &mockEngine{
		volumes: map[string]bool{"omnideck-home": true},
	})
	if r.Status != CheckPass {
		t.Errorf("existing volume should be CheckPass, got status %d detail=%q", r.Status, r.Detail)
	}
}

func TestDoctorUsesSharedRuntimeDiagnosisAndGuidedAction(t *testing.T) {
	results, _ := workflow.DiagnoseWithProbes(
		"omnideck",
		&config.Config{ContainerName: "omnideck", WebUIPort: "2337"},
		&mockEngine{name: "podman"},
		[]engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
		},
	)
	if len(results) == 0 {
		t.Fatal("Doctor returned no results")
	}
	runtimeResult := results[0]
	if runtimeResult.Status != CheckFail || runtimeResult.Action != DoctorActionRuntimeSetup || runtimeResult.ActionValue != "podman" {
		t.Fatalf("runtime result = %#v", runtimeResult)
	}
	combined := runtimeResult.Detail + " " + runtimeResult.Hint
	if strings.Contains(combined, "usermod") || strings.Contains(combined, "sudo") {
		t.Fatalf("Doctor must not contradict safe setup with an account-changing command: %s", combined)
	}
	for _, result := range results[1:] {
		if result.Label == "Omnideck instance" && result.Status != CheckInfo {
			t.Fatalf("dependent checks should not report a second failure: %#v", result)
		}
	}
}

func TestDoctorOffersStartForAStoppedInstance(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck", WebUIPort: "2337", Image: "example:test"}
	results, _ := workflow.DiagnoseWithProbes("omnideck", cfg, &mockEngine{name: "podman", containerExists: true, containerStatus: "exited"}, []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeReady, Version: "6.0.2"},
	})
	if results[0].Status != CheckPass {
		t.Fatalf("runtime should pass: %#v", results[0])
	}
	instance := results[1]
	if instance.Status != CheckFail || instance.Action != DoctorActionStartInstance || instance.ActionLabel != "Start Omnideck" {
		t.Fatalf("stopped instance result = %#v", instance)
	}
	if results[2].Status != CheckInfo || !strings.Contains(results[2].Detail, "until Omnideck is running") {
		t.Fatalf("browser check should explain its dependency: %#v", results[2])
	}
}

// Starting a container never adds a missing sign-in address, so Doctor must
// report it for a stopped container too, not only after it is started.
func TestDoctorChecksSignInAddressOfAStoppedInstance(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck", WebUIPort: "2337", Image: "example:test"}
	results, _ := workflow.DiagnoseWithProbes("omnideck", cfg, &mockEngine{
		name: "podman", containerExists: true, containerStatus: "exited", env: []string{"PORT=8080"},
	}, []engine.ProbeResult{{Name: "podman", State: engine.RuntimeReady, Version: "6.0.2"}})
	for _, result := range results {
		if result.Label == "MCP sign-in address" {
			if result.Status != CheckWarn {
				t.Fatalf("sign-in address result = %#v", result)
			}
			return
		}
	}
	t.Fatalf("no sign-in address check for a stopped container: %#v", results)
}

func TestDoctorOffersRepairForAMissingContainer(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck", WebUIPort: "2337", Image: "example:test"}
	results, _ := workflow.DiagnoseWithProbes("omnideck", cfg, &mockEngine{
		name: "podman",
		volumes: map[string]bool{
			cfg.HomeVolumeName():  true,
			cfg.StateVolumeName(): true,
		},
	}, []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeReady, Version: "6.0.2"},
	})
	instance := results[1]
	if instance.Status != CheckFail || instance.Action != DoctorActionRepairInstance || instance.ActionLabel != "Repair this installation" {
		t.Fatalf("missing instance result = %#v", instance)
	}
	if !strings.Contains(instance.Hint, "same saved data volumes") {
		t.Fatalf("repair safety explanation = %q", instance.Hint)
	}
}

func TestDoctorDoesNotOfferRepairWhenSavedStorageIsMissing(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck", WebUIPort: "2337", Image: "example:test"}
	results, _ := workflow.DiagnoseWithProbes("omnideck", cfg, &mockEngine{
		name:    "podman",
		volumes: map[string]bool{cfg.StateVolumeName(): true},
	}, []engine.ProbeResult{{Name: "podman", State: engine.RuntimeReady}})

	instance := results[1]
	if instance.Action != DoctorActionNone {
		t.Fatalf("unsafe repair action remained available: %#v", instance)
	}
	if !strings.Contains(instance.Hint, "Automatic repair is paused") {
		t.Fatalf("repair safety hint = %q", instance.Hint)
	}
}

func TestDoctorTreatsOptionalInformationAsHealthy(t *testing.T) {
	results := []CheckResult{
		{Label: "Runtime", Status: CheckPass, Detail: "ready"},
		{Label: "Local AI (optional)", Status: CheckInfo, Detail: "not connected"},
		{Label: "Memory", Status: CheckWarn, Detail: "low"},
	}
	report, allPass := RenderDoctorReport(results)
	if !allPass || !strings.Contains(report, "Everything required") {
		t.Fatalf("optional information and warnings should not claim Omnideck is broken:\n%s", report)
	}
}

func TestDoctorDashboardCanOpenGuidedRuntimeRepair(t *testing.T) {
	instances := []config.InstanceInfo{{
		Name: "omnideck",
		Path: "/tmp/omnideck.yaml",
		Config: &config.Config{
			ContainerName: "omnideck",
			WebUIPort:     "2337",
		},
	}}
	m := NewAppModel(&mockEngine{name: "podman"}, instances)
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{{
		Label:       "Container runtime",
		Status:      CheckFail,
		Detail:      "Podman is not running",
		Action:      DoctorActionRuntimeSetup,
		ActionLabel: "Start Podman",
		ActionValue: "podman",
	}}
	m.doctorFocus = 0

	newModel, cmd := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(AppModel)
	if cmd == nil || nm.router.Current() != RouteSetup {
		t.Fatal("Doctor action should open guided runtime setup")
	}
	if nm.setupModel.setupMode != SetupRuntimeRepair {
		t.Fatalf("Doctor opened the wrong setup journey: mode=%d", nm.setupModel.setupMode)
	}
}

func TestDoctorDashboardPresentsActionsWithoutTruncatingTheFix(t *testing.T) {
	m := NewAppModel(nil, nil)
	m.width, m.height = 100, 36
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{
		{
			Label:       "Container runtime",
			Status:      CheckFail,
			Detail:      "Podman is installed, but it is not running yet",
			Hint:        "Omnideck can walk you through starting Podman and check it again afterward.",
			Action:      DoctorActionRuntimeSetup,
			ActionLabel: "Start Podman",
		},
		{Label: "Local AI (optional)", Status: CheckInfo, Detail: "Not connected · online AI still works"},
	}
	m.doctorFocus = 0

	view := m.viewDoctor()
	for _, want := range []string{"1 problem needs attention", "What you can do", "Press Enter to start podman", "online AI still works"} {
		if !strings.Contains(strings.ToLower(view), strings.ToLower(want)) {
			t.Fatalf("Doctor view is missing %q:\n%s", want, view)
		}
	}
}

func TestDoctorDashboardOpensRepairReviewForMissingContainer(t *testing.T) {
	instances := []config.InstanceInfo{{
		Name: "omnideck",
		Path: "/tmp/omnideck.yaml",
		Config: &config.Config{
			ContainerName: "omnideck",
			WebUIPort:     "2337",
			Image:         "example:test",
		},
	}}
	m := NewAppModel(&mockEngine{name: "podman"}, instances)
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{{
		Label:       "Omnideck instance",
		Status:      CheckFail,
		Action:      DoctorActionRepairInstance,
		ActionLabel: "Repair this installation",
	}}
	m.doctorFocus = 0

	newModel, cmd := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(AppModel)
	if cmd != nil || nm.router.Current() != RouteMaintenance || nm.maintenanceModel.Mode != MaintenanceRepair || nm.maintenanceModel.Stage != MaintenanceStageReview {
		t.Fatalf("repair action = route %d mode %d stage %d cmd %v", nm.router.Current(), nm.maintenanceModel.Mode, nm.maintenanceModel.Stage, cmd)
	}
	if !strings.Contains(nm.maintenanceModel.TUIView(80), "same saved file and app-data volumes") {
		t.Fatal("repair review must explain that saved data is reconnected")
	}
}

func TestDoctorActionNavigationSkipsInformationalRows(t *testing.T) {
	results := []CheckResult{
		{Label: "Runtime", Status: CheckFail, Action: DoctorActionRuntimeSetup},
		{Label: "System", Status: CheckInfo},
		{Label: "Instance", Status: CheckFail, Action: DoctorActionStartInstance},
	}
	if got := firstDoctorAction(results); got != 0 {
		t.Fatalf("first action = %d, want 0", got)
	}
	if got := nextDoctorAction(results, 0, 1); got != 2 {
		t.Fatalf("next action = %d, want 2", got)
	}
	if got := nextDoctorAction(results, 2, 1); got != 0 {
		t.Fatalf("wrapped action = %d, want 0", got)
	}
}

// Desktop's instance file is "desktop" while its container is
// "omnideck-desktop"; Doctor's hints and actionValue must name the instance,
// because that is what --name accepts.
func TestDoctorHintsNameTheInstanceNotTheContainer(t *testing.T) {
	cfg := &config.Config{ContainerName: "omnideck-desktop", WebUIPort: "2337", Image: "example:test"}
	results, _ := workflow.DiagnoseWithProbes("desktop", cfg, &mockEngine{name: "podman", containerExists: true, containerStatus: "exited"}, []engine.ProbeResult{
		{Name: "podman", State: engine.RuntimeReady, Version: "6.0.2"},
	})
	instance := results[1]
	if instance.Hint != "Run: omnideck start --name desktop" || instance.ActionValue != "desktop" {
		t.Fatalf("stopped instance result = %#v", instance)
	}
	if got := doctorActionCommand(instance); got != "run `omnideck start --name desktop`" {
		t.Fatalf("plain report command = %q", got)
	}
}

func TestDoctorStartActionStartsTheCheckedInstancesContainer(t *testing.T) {
	eng := &mockEngine{name: "podman", containerExists: true, containerStatus: "exited"}
	m := NewAppModel(eng, []config.InstanceInfo{{
		Name:   "desktop",
		Config: &config.Config{ContainerName: "omnideck-desktop", WebUIPort: "2337", Image: "example:test"},
	}})
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{{
		Label:       "Omnideck instance",
		Status:      CheckFail,
		Action:      DoctorActionStartInstance,
		ActionLabel: "Start Omnideck",
		ActionValue: "desktop",
	}}
	m.doctorFocus = 0

	_, cmd := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("start action returned no work")
	}
	if msg, ok := cmd().(doctorActionDoneMsg); !ok || msg.err != nil {
		t.Fatalf("start action result = %#v", msg)
	}
	if len(eng.startedNames) != 1 || eng.startedNames[0] != "omnideck-desktop" {
		t.Fatalf("started %v, want the instance's container omnideck-desktop", eng.startedNames)
	}
}

func TestDoctorStartActionExplainsUnreadableSettings(t *testing.T) {
	eng := &mockEngine{name: "podman", containerExists: true, containerStatus: "exited"}
	m := NewAppModel(eng, []config.InstanceInfo{{Name: "desktop"}})
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{{
		Label:       "Omnideck instance",
		Status:      CheckFail,
		Action:      DoctorActionStartInstance,
		ActionLabel: "Start Omnideck",
		ActionValue: "desktop",
	}}
	m.doctorFocus = 0

	updated, cmd := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || len(eng.startedNames) != 0 {
		t.Fatalf("start ran without saved settings: cmd=%v started=%v", cmd != nil, eng.startedNames)
	}
	if got := updated.(AppModel).doctorMessage; !strings.Contains(got, "could not read") {
		t.Fatalf("doctorMessage = %q, want an explanation", got)
	}
}

func TestDoctorUpdateActionOpensTheUpdateReview(t *testing.T) {
	eng := &mockEngine{name: "podman", containerExists: true, containerStatus: "running"}
	m := NewAppModel(eng, []config.InstanceInfo{{
		Name:   "desktop",
		Config: &config.Config{ContainerName: "omnideck-desktop", WebUIPort: "2337", Image: "example:test"},
	}})
	m.router.Replace(RouteDoctor)
	m.doctorStage = doctorStageResults
	m.doctorResults = []CheckResult{{
		Label: "MCP sign-in address", Status: CheckWarn,
		Action: DoctorActionUpdateInstance, ActionLabel: "Update Omnideck", ActionValue: "desktop",
	}}
	m.doctorFocus = 0

	updated, _ := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	app := updated.(AppModel)
	if app.router.Current() != RouteMaintenance || app.maintenanceModel.Mode != MaintenanceUpdate || app.maintenanceModel.Stage != MaintenanceStageReview {
		t.Fatalf("route = %v mode = %v stage = %v, want the update review", app.router.Current(), app.maintenanceModel.Mode, app.maintenanceModel.Stage)
	}
	if got := doctorActionCommand(m.doctorResults[0]); got != "run `omnideck update --name desktop`" {
		t.Fatalf("plain report command = %q", got)
	}
	if got := doctorActionCommand(CheckResult{Action: DoctorActionRepairInstance, ActionValue: "desktop"}); got != "run `omnideck repair --name desktop`" {
		t.Fatalf("plain repair command = %q", got)
	}
}

// The memory rule is judged on the final values, so lowering memory and
// shared memory together in one edit is accepted.
func TestSettingsJudgeMemoryLimitsOnTheFinalValues(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "podman"}, []config.InstanceInfo{{
		Name:   "work",
		Config: &config.Config{ContainerName: "work", Memory: "2g", ShmSize: "1g", WebUIPort: "2337", Image: "example:test"},
	}})
	m.settingFields = []settingField{
		{Key: "memory", Value: "512m", Changed: true},
		{Key: "shm_size", Value: "256m", Changed: true},
	}
	if err := m.validateSettingFields(); err != nil {
		t.Fatalf("lowering both together was rejected: %v", err)
	}
	if cfg := m.configFromSettingFields(); cfg.Memory != "512m" || cfg.ShmSize != "256m" {
		t.Fatalf("saved candidate = %s/%s", cfg.Memory, cfg.ShmSize)
	}
	m.settingFields = []settingField{{Key: "shm_size", Value: "4g", Changed: true}}
	if err := m.validateSettingFields(); err == nil {
		t.Fatal("shared memory above memory was accepted")
	}
}

func TestDoctorReportPrintsEachCommandOnce(t *testing.T) {
	out, _ := RenderDoctorReport([]CheckResult{{
		Label: "Omnideck instance", Status: CheckFail, Detail: "missing",
		Hint:   "Run: omnideck repair --name desktop (recreates the container)",
		Action: DoctorActionRepairInstance, ActionValue: "desktop",
	}})
	if n := strings.Count(out, "omnideck repair --name desktop"); n != 1 {
		t.Fatalf("command printed %d times:\n%s", n, out)
	}
}

func TestSettingsNameTheMemoryFieldThatChanged(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "podman"}, []config.InstanceInfo{{
		Name:   "work",
		Config: &config.Config{ContainerName: "work", Memory: "4g", ShmSize: "1g", WebUIPort: "2337", Image: "example:test"},
	}})
	m.settingFields = []settingField{{Key: "memory", Value: "512m", Changed: true}}
	err := m.validateSettingFields()
	if err == nil || !strings.Contains(err.Error(), "lower shm_size first") {
		t.Fatalf("err = %v, want it to name memory", err)
	}
}

// Older CLIs saved shared memory above memory. Like `config set`, Settings
// only applies the rule once memory or shm_size is edited.
func TestSettingsAcceptUnrelatedEditsToOlderMemorySettings(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "podman"}, []config.InstanceInfo{{
		Name:   "work",
		Config: &config.Config{ContainerName: "work", Memory: "1g", ShmSize: "2g", WebUIPort: "2337", Image: "example:test"},
	}})
	m.settingFields = []settingField{{Key: "image", Value: "example:v2", Changed: true}}
	if err := m.validateSettingFields(); err != nil {
		t.Fatalf("an unrelated edit was rejected: %v", err)
	}
	m.settingFields = []settingField{{Key: "shm_size", Value: "3g", Changed: true}}
	if err := m.validateSettingFields(); err == nil {
		t.Fatal("editing shm_size above memory was accepted")
	}
}
