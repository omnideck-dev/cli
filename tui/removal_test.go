package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
)

func removalTestModel() RemovalModel {
	cfg := config.DefaultConfig()
	return NewRemovalModel(RemovalRequest{
		Instance: config.InstanceInfo{Name: "omnideck", Path: "omnideck.yaml", Config: cfg},
		Engine:   &mockEngine{containerExists: true, containerStatus: "running", volumes: map[string]bool{}},
		Embedded: true,
	})
}

func TestRemovalDefaultsToKeepingSavedData(t *testing.T) {
	m := removalTestModel()
	view := m.TNView(80)
	for _, want := range []string{
		"Keep saved data — Recommended",
		"your container runtime will stay installed.",
		"Permanently delete saved data",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial removal screen does not contain %q:\n%s", want, view)
		}
	}

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(RemovalModel)
	if cmd != nil || m.Stage != RemovalStageReview || m.deleteData {
		t.Fatalf("safe choice = stage %d delete data %v command %v", m.Stage, m.deleteData, cmd)
	}
	if !strings.Contains(m.TNView(80), "Keep its saved files and agent data") {
		t.Fatal("safe review does not explain that saved data will be kept")
	}
}

func TestRemovalRequiresExactNameBeforeDeletingData(t *testing.T) {
	m := removalTestModel()
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(RemovalModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(RemovalModel)
	if m.Stage != RemovalStageBackupChoice {
		t.Fatalf("delete choice opened stage %d", m.Stage)
	}

	// Creating a backup is the safe default.
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(RemovalModel)
	if m.Stage != RemovalStageDeleteConfirm || !m.backupData {
		t.Fatalf("backup choice = stage %d backup %v", m.Stage, m.backupData)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(RemovalModel)
	if m.Stage != RemovalStageDeleteConfirm || m.inputError == "" {
		t.Fatal("empty confirmation advanced to permanent deletion")
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("omnideck")})
	m = model.(RemovalModel)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(RemovalModel)
	if m.Stage != RemovalStageApplying || cmd == nil {
		t.Fatalf("exact confirmation = stage %d command %v", m.Stage, cmd)
	}
}

func TestDashboardRemoveShortcutOpensFullScreenWorkflow(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "docker"}, oneTestInstance())
	model, cmd := m.Update(runeKey('x'))
	m = model.(AppModel)
	if cmd != nil || m.router.Current() != RouteRemoval || m.dialog != nil {
		t.Fatalf("remove shortcut = route %d dialog %v command %v", m.router.Current(), m.dialog != nil, cmd)
	}
	if m.removalModel.Stage != RemovalStageDataChoice || m.removalModel.instanceName() != "omnideck" {
		t.Fatalf("removal model = stage %d instance %q", m.removalModel.Stage, m.removalModel.instanceName())
	}
}

func TestExpandedInstanceExposesRemoveAction(t *testing.T) {
	m := NewAppModel(&mockEngine{name: "docker"}, oneTestInstance())
	m.width = 120
	m.height = 40
	m.expanded["omnideck"] = true
	if view := m.viewDashboard(); !strings.Contains(view, "Remove instance") {
		t.Fatalf("expanded instance does not show its remove action:\n%s", view)
	}

	m.chipFocus = 3
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(AppModel)
	if m.chipFocus != 4 {
		t.Fatalf("tab selected action %d, want remove action", m.chipFocus)
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(AppModel)
	if m.router.Current() != RouteRemoval {
		t.Fatalf("remove action opened route %d", m.router.Current())
	}
}
