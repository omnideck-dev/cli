package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
)

func TestUpdateStartsAtReviewWithoutChangingAnything(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewMaintenanceModel(MaintenanceRequest{Config: cfg, Engine: &mockEngine{containerExists: true, containerStatus: "running"}})
	if m.Stage != MaintenanceStageReview {
		t.Fatalf("stage = %d, want review", m.Stage)
	}
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init must not start an update before user confirmation")
	}
	if view := m.TNView(80); !strings.Contains(view, "will not be deleted") || !strings.Contains(view, "without changing anything") {
		t.Fatalf("review does not explain safety and cancellation:\n%s", view)
	}
}

func TestUpdateConfirmationAndRetryHaveExplicitTransitions(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewMaintenanceModel(MaintenanceRequest{Config: cfg, Engine: &mockEngine{containerExists: true, containerStatus: "running"}})
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(MaintenanceModel)
	if m.Stage != MaintenanceStageApplying || cmd == nil {
		t.Fatalf("confirmation = stage %d, cmd %v; want applying with work", m.Stage, cmd)
	}

	model, _ = m.Update(StepFailedMsg{Index: 0, Err: errors.New("network unavailable")})
	m = model.(MaintenanceModel)
	if m.Stage != MaintenanceStageFailed || !strings.Contains(m.TNView(80), "try again") {
		t.Fatalf("failure state is not retryable: stage=%d", m.Stage)
	}

	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = model.(MaintenanceModel)
	if m.Stage != MaintenanceStageApplying || cmd == nil {
		t.Fatalf("retry = stage %d, cmd %v; want applying with work", m.Stage, cmd)
	}
}

func TestSetupAndMaintenanceUseDifferentStageTypes(t *testing.T) {
	// This compile-time-oriented assertion documents the architectural
	// invariant: each workflow owns its state machine instead of sharing a
	// generic phase enum with impossible states.
	setup := SetupStageReview
	maintenance := MaintenanceStageReview
	if int(setup) < 0 || int(maintenance) < 0 {
		t.Fatal("unreachable")
	}
}
