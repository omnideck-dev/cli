package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDialogDefaultsToTheSafeChoice(t *testing.T) {
	dialog := NewDiscardSettingsDialog()
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	result := cmd().(DialogResultMsg)
	if result.Confirmed {
		t.Fatal("Enter must keep editing until the user explicitly selects discard")
	}

	dialog, _ = dialog.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, cmd = dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	result = cmd().(DialogResultMsg)
	if !result.Confirmed {
		t.Fatal("selected discard action should confirm")
	}
}
