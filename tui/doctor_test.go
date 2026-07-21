package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

func TestRenderDoctorReportAllPass(t *testing.T) {
	results := []CheckResult{
		{Label: "Engine", Status: CheckPass, Detail: "Docker 24.0"},
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
		{Label: "Engine", Status: CheckFail, Detail: "not found", Hint: "Install Docker"},
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
	r := volumeCheck("Test volume", "missing-volume", &mockEngine{})
	if r.Status != CheckFail {
		t.Error("missing volume should be CheckFail")
	}
}

func TestVolumeCheckExists(t *testing.T) {
	r := volumeCheck("Test volume", "omnideck-home", &mockEngine{
		volumes: map[string]bool{"omnideck-home": true},
	})
	if r.Status != CheckPass {
		t.Errorf("existing volume should be CheckPass, got status %d detail=%q", r.Status, r.Detail)
	}
}

func TestDoctorUsesSharedRuntimeDiagnosisAndGuidedAction(t *testing.T) {
	results := runDoctorChecksWithProbes(
		&config.Config{ContainerName: "omnideck", WebUIPort: "2337"},
		&mockEngine{name: "docker"},
		[]engine.ProbeResult{
			{Name: "podman", State: engine.RuntimeMissing},
			{Name: "docker", State: engine.RuntimePermissionDenied},
		},
	)
	if len(results) == 0 {
		t.Fatal("Doctor returned no results")
	}
	runtimeResult := results[0]
	if runtimeResult.Status != CheckFail || runtimeResult.Action != DoctorActionRuntimeSetup || runtimeResult.ActionValue != "docker" {
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
	results := runDoctorChecksWithProbes(cfg, &mockEngine{name: "docker", containerStatus: "exited"}, []engine.ProbeResult{
		{Name: "docker", State: engine.RuntimeReady, Version: "27.0.0"},
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
	m := NewDashboardModel(&mockEngine{name: "docker"}, instances)
	m.screen = ScreenDoctor
	m.doctorDone = true
	m.doctorResults = []CheckResult{{
		Label:       "Container runtime",
		Status:      CheckFail,
		Detail:      "Docker is not running",
		Action:      DoctorActionRuntimeSetup,
		ActionLabel: "Start Docker",
		ActionValue: "docker",
	}}
	m.doctorFocus = 0

	newModel, cmd := m.updateDoctor(tea.KeyMsg{Type: tea.KeyEnter})
	nm := newModel.(DashboardModel)
	if cmd == nil || nm.screen != ScreenInstall {
		t.Fatal("Doctor action should open guided runtime setup")
	}
	if nm.installModel.setupMode != SetupRuntimeRepair || nm.installModel.preferredEngine != "docker" {
		t.Fatalf("Doctor opened the wrong setup journey: mode=%d preferred=%q", nm.installModel.setupMode, nm.installModel.preferredEngine)
	}
}

func TestDoctorDashboardPresentsActionsWithoutTruncatingTheFix(t *testing.T) {
	m := NewDashboardModel(nil, nil)
	m.width, m.height = 100, 36
	m.screen = ScreenDoctor
	m.doctorDone = true
	m.doctorResults = []CheckResult{
		{
			Label:       "Container runtime",
			Status:      CheckFail,
			Detail:      "Docker is installed, but it is not running yet",
			Hint:        "Omnideck can walk you through starting Docker and check it again afterward.",
			Action:      DoctorActionRuntimeSetup,
			ActionLabel: "Start Docker",
		},
		{Label: "Local AI (optional)", Status: CheckInfo, Detail: "Not connected · online AI still works"},
	}
	m.doctorFocus = 0

	view := m.viewDoctor()
	for _, want := range []string{"1 problem needs attention", "What you can do", "Press Enter to start docker", "online AI still works"} {
		if !strings.Contains(strings.ToLower(view), strings.ToLower(want)) {
			t.Fatalf("Doctor view is missing %q:\n%s", want, view)
		}
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
