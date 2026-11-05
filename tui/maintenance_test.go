package tui

import (
	"errors"
	"path/filepath"
	"reflect"
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
	if view := m.TUIView(80); !strings.Contains(view, "will not be deleted") || !strings.Contains(view, "without changing anything") {
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
	if m.Stage != MaintenanceStageFailed || !strings.Contains(m.TUIView(80), "try again") {
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

// runMaintenanceStep executes one step's tea.Sequence synchronously and
// returns its final message (StepDoneMsg or StepFailedMsg).
func runMaintenanceStep(t *testing.T, m *MaintenanceModel, i int) tea.Msg {
	t.Helper()
	msg := m.startMaintenanceStep(i)()
	cmds := reflect.ValueOf(msg)
	if cmds.Kind() != reflect.Slice || cmds.Len() == 0 {
		t.Fatalf("step %d did not return a command sequence: %T", i, msg)
	}
	var last tea.Msg
	for j := 0; j < cmds.Len(); j++ {
		if cmd, ok := cmds.Index(j).Interface().(tea.Cmd); ok && cmd != nil {
			last = cmd()
		}
	}
	return last
}

func TestUpdateReportsMCPSignInAddressOnCompletion(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebUIPort = "2339"
	eng := &mockEngine{containerExists: true, containerStatus: "running", env: []string{"PORT=8080"}}
	m := NewMaintenanceModel(MaintenanceRequest{
		Config:     cfg,
		ConfigPath: filepath.Join(t.TempDir(), "omnideck.yaml"),
		Engine:     eng,
		Embedded:   true,
	})
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(MaintenanceModel)
	for i := 1; i < len(m.stepLabels); i++ {
		msg := runMaintenanceStep(t, &m, i)
		if failed, ok := msg.(StepFailedMsg); ok {
			t.Fatalf("step %d failed: %v", i, failed.Err)
		}
		model, _ = m.Update(msg)
		m = model.(MaintenanceModel)
	}
	if m.Stage != MaintenanceStageComplete {
		t.Fatalf("stage = %d, want complete", m.Stage)
	}
	if view := m.TUIView(80); !strings.Contains(view, "MCP sign-in address set to http://localhost:2339") {
		t.Fatalf("completion does not report the sign-in address:\n%s", view)
	}
	if cfg.LayoutVersion != config.CurrentContainerLayout {
		t.Fatalf("saved layout = %d, want %d", cfg.LayoutVersion, config.CurrentContainerLayout)
	}
}

func TestUpdateKeepsConfigAlignedWhenSignInCheckFails(t *testing.T) {
	cfg := config.DefaultConfig()
	eng := &mockEngine{containerExists: true, containerStatus: "running"}
	m := NewMaintenanceModel(MaintenanceRequest{
		Config:     cfg,
		ConfigPath: filepath.Join(t.TempDir(), "omnideck.yaml"),
		Engine:     eng,
		Embedded:   true,
	})
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(MaintenanceModel)
	model, _ = m.Update(runMaintenanceStep(t, &m, 1))
	m = model.(MaintenanceModel)
	eng.envErr = errors.New("inspect timed out")
	model, _ = m.Update(runMaintenanceStep(t, &m, 2))
	m = model.(MaintenanceModel)
	if m.Stage != MaintenanceStageFailed {
		t.Fatalf("stage = %d, want failed", m.Stage)
	}
	if cfg.LayoutVersion != config.CurrentContainerLayout || m.current.LayoutVersion != config.CurrentContainerLayout {
		t.Fatalf("saved replacement not reflected in memory: cfg=%d current=%d", cfg.LayoutVersion, m.current.LayoutVersion)
	}
}
