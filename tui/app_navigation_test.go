package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
)

func runeKey(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}

func oneTestInstance() []config.InstanceInfo {
	cfg := config.DefaultConfig()
	return []config.InstanceInfo{{Name: cfg.ContainerName, Path: "omnideck.yaml", Config: cfg}}
}

func TestAppRoutesToAFullScreenAndBack(t *testing.T) {
	m := NewAppModel(nil, oneTestInstance())
	model, _ := m.Update(runeKey('l'))
	m = model.(AppModel)
	if m.router.Current() != RouteLogs || !m.router.CanGoBack() {
		t.Fatalf("logs navigation = route %d, back %v", m.router.Current(), m.router.CanGoBack())
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(AppModel)
	if m.router.Current() != RouteDashboard || m.router.CanGoBack() {
		t.Fatalf("logs back = route %d, back %v", m.router.Current(), m.router.CanGoBack())
	}
}

func TestDashboardUpdateShortcutOpensMaintenanceScreen(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "docker"}, oneTestInstance())
	model, _ := m.Update(runeKey('u'))
	m = model.(AppModel)
	if m.router.Current() != RouteMaintenance || m.maintenanceModel.Stage != MaintenanceStageReview {
		t.Fatalf("update shortcut = route %d stage %d", m.router.Current(), m.maintenanceModel.Stage)
	}
}

func TestNestedWorkflowReturnsToItsCallingScreen(t *testing.T) {
	m := NewAppModel(nil, oneTestInstance())
	m.router.Push(RouteDoctor)
	m.router.Push(RouteMaintenance)

	model, _ := m.Update(WorkflowExitMsg{Outcome: WorkflowCanceled})
	m = model.(AppModel)
	if m.router.Current() != RouteDoctor {
		t.Fatalf("maintenance cancel returned to route %d, want Doctor", m.router.Current())
	}
}

func TestRootWorkflowCancelQuitsInsteadOfShowingAnEmptyDashboard(t *testing.T) {
	m := NewAppModelForSetup(nil, nil, "", "")
	_, cmd := m.Update(WorkflowExitMsg{Outcome: WorkflowCanceled})
	if cmd == nil {
		t.Fatal("root-level cancel should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("root-level cancel emitted %T, want tea.QuitMsg", cmd())
	}
}

func TestSettingsBackProtectsUnsavedChanges(t *testing.T) {
	m := NewAppModel(nil, oneTestInstance())
	model, _ := m.Update(runeKey('c'))
	m = model.(AppModel)
	m.settingFields[0].Value = "changed-home"
	m.settingFields[0].Changed = true

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(AppModel)
	if m.dialog == nil || m.router.Current() != RouteSettings {
		t.Fatalf("settings back = dialog %v route %d", m.dialog != nil, m.router.Current())
	}

	// Enter chooses the safe default: keep editing.
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(AppModel)
	model, _ = m.Update(cmd())
	m = model.(AppModel)
	if m.dialog != nil || m.router.Current() != RouteSettings {
		t.Fatalf("safe dialog choice = dialog %v route %d", m.dialog != nil, m.router.Current())
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(AppModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(AppModel)
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(AppModel)
	model, _ = m.Update(cmd())
	m = model.(AppModel)
	if m.dialog != nil || m.router.Current() != RouteDashboard || m.settingFields != nil {
		t.Fatalf("confirmed discard = dialog %v route %d fields %d", m.dialog != nil, m.router.Current(), len(m.settingFields))
	}
}

func TestSetupCancelEmitsAnExplicitCanceledOutcome(t *testing.T) {
	m := NewSetupModel(SetupRequest{Initial: config.DefaultConfig(), Embedded: true})
	_, cmd := m.Update(runeKey('q'))
	if cmd == nil {
		t.Fatal("setup cancel did not emit a command")
	}
	exit, ok := cmd().(WorkflowExitMsg)
	if !ok || exit.Outcome != WorkflowCanceled {
		t.Fatalf("setup cancel emitted %#v", cmd())
	}
}

func TestSetupReviewEscapeGoesBackWithoutCanceling(t *testing.T) {
	m := NewSetupModel(SetupRequest{Initial: config.DefaultConfig(), Embedded: true})
	m.Stage = SetupStageReview
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(SetupModel)
	if cmd != nil || m.Stage != SetupStageSettings {
		t.Fatalf("review escape = stage %d command %v", m.Stage, cmd)
	}
}

func TestRuntimeCommandCannotBeCanceledMidStep(t *testing.T) {
	m := NewSetupModel(SetupRequest{Initial: config.DefaultConfig(), Embedded: true})
	m.Stage = SetupStageRuntime
	m.runtimeSetupStage = runtimeSetupWorking
	model, cmd := m.Update(runeKey('q'))
	m = model.(SetupModel)
	if cmd != nil || m.Stage != SetupStageRuntime || m.runtimeSetupStage != runtimeSetupWorking {
		t.Fatalf("runtime command cancel = stage %d runtime stage %d command %v", m.Stage, m.runtimeSetupStage, cmd)
	}
}

func TestDoctorActionMustFinishBeforeGoingBack(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "docker"}, oneTestInstance())
	m.router.Push(RouteDoctor)
	m.doctorStage = doctorStageActing
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(AppModel)
	if cmd != nil || m.router.Current() != RouteDoctor {
		t.Fatalf("doctor action escape = route %d command %v", m.router.Current(), cmd)
	}
}
