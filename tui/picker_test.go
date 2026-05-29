package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
)

func makePickerItems() []config.InstanceInfo {
	return []config.InstanceInfo{
		{Name: "alpha", Path: "/tmp/alpha.yaml", Config: &config.Config{ContainerName: "alpha", Engine: "docker"}},
		{Name: "beta", Path: "/tmp/beta.yaml", Config: &config.Config{ContainerName: "beta", Engine: "podman"}},
		{Name: "gamma", Path: "/tmp/gamma.yaml", Config: &config.Config{ContainerName: "gamma", Engine: "docker"}},
	}
}

func TestPickerInit(t *testing.T) {
	m := NewPickerModel("Pick one", "subtitle", makePickerItems())
	if m.cursor != 0 {
		t.Errorf("initial cursor should be 0, got %d", m.cursor)
	}
	if m.chosen != -1 {
		t.Errorf("chosen should be -1 initially, got %d", m.chosen)
	}
}

func TestPickerNavigateDown(t *testing.T) {
	m := NewPickerModel("", "", makePickerItems())
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.cursor != 1 {
		t.Errorf("after j: cursor should be 1, got %d", m.cursor)
	}
}

func TestPickerNavigateUp(t *testing.T) {
	m := NewPickerModel("", "", makePickerItems())
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursor != 0 {
		t.Errorf("after j then k: cursor should be 0, got %d", m.cursor)
	}
}

func TestPickerNoWrapAtBounds(t *testing.T) {
	m := NewPickerModel("", "", makePickerItems())
	// Navigate up at top — should stay at 0.
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.cursor != 0 {
		t.Errorf("cursor should not go below 0")
	}
	// Navigate past end — should stop at last.
	for i := 0; i < 10; i++ {
		m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if m.cursor != len(makePickerItems())-1 {
		t.Errorf("cursor should not exceed last item, got %d", m.cursor)
	}
}

func TestPickerEnterSelects(t *testing.T) {
	m := NewPickerModel("", "", makePickerItems())
	// Navigate to beta.
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.chosen != 1 {
		t.Errorf("chosen should be 1 after Enter on beta, got %d", m.chosen)
	}
	inst, ok := m.Chosen()
	if !ok {
		t.Fatal("Chosen() should return ok=true")
	}
	if inst.Name != "beta" {
		t.Errorf("Chosen name: got %q, want 'beta'", inst.Name)
	}
}

func TestPickerQuitNoSelection(t *testing.T) {
	m := NewPickerModel("", "", makePickerItems())
	m = updatePicker(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_, ok := m.Chosen()
	if ok {
		t.Error("Chosen() should return ok=false after quit")
	}
}

func TestPickerViewContainsNames(t *testing.T) {
	m := NewPickerModel("Pick one", "choose", makePickerItems())
	view := m.View()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !containsStr(view, name) {
			t.Errorf("view should contain %q", name)
		}
	}
}

// updatePicker is a typed helper that unwraps Update's tea.Model return.
func updatePicker(m PickerModel, msg tea.Msg) PickerModel {
	result, _ := m.Update(msg)
	return result.(PickerModel)
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
