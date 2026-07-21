package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

type DialogAction int

const (
	DialogDiscardSettings DialogAction = iota
)

type DialogResultMsg struct {
	Action    DialogAction
	Confirmed bool
}

// ConfirmDialog is a true blocking dialog: while it is present, the active
// screen does not receive input. It is reserved for short decisions rather
// than substantial application journeys.
type ConfirmDialog struct {
	Title        string
	Message      string
	ConfirmLabel string
	CancelLabel  string
	Action       DialogAction
	focusConfirm bool
}

func NewDiscardSettingsDialog() ConfirmDialog {
	return ConfirmDialog{
		Title:        "Discard changes?",
		Message:      "You changed one or more settings. Going back now will forget those changes.",
		ConfirmLabel: "Discard changes",
		CancelLabel:  "Keep editing",
		Action:       DialogDiscardSettings,
	}
}

func (d ConfirmDialog) Update(msg tea.Msg) (ConfirmDialog, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch key.String() {
	case "left", "right", "tab", "shift+tab":
		d.focusConfirm = !d.focusConfirm
	case "enter", " ":
		confirmed := d.focusConfirm
		return d, func() tea.Msg { return DialogResultMsg{Action: d.Action, Confirmed: confirmed} }
	case "esc", "b", "q":
		return d, func() tea.Msg { return DialogResultMsg{Action: d.Action, Confirmed: false} }
	case "d", "y":
		return d, func() tea.Msg { return DialogResultMsg{Action: d.Action, Confirmed: true} }
	}
	return d, nil
}

func (d ConfirmDialog) View(width int) string {
	contentWidth := min(64, max(28, width-12))
	message := strings.Join(wrapWords(d.Message, contentWidth-4, contentWidth-4), "\n")
	confirmStyle := styles.TNDimText
	cancelStyle := styles.TNBlueTxt
	confirmCursor, cancelCursor := "  ", "▸ "
	if d.focusConfirm {
		confirmStyle, cancelStyle = styles.TNRedTxt, styles.TNDimText
		confirmCursor, cancelCursor = "▸ ", "  "
	}
	body := styles.TNTextBold.Render(d.Title) + "\n\n" +
		styles.TNDimText.Render(message) + "\n\n" +
		confirmStyle.Render(confirmCursor+d.ConfirmLabel) + "    " +
		cancelStyle.Render(cancelCursor+d.CancelLabel)
	return styles.TNPanelAccent.Width(contentWidth).Padding(1, 2).Render(body)
}

func renderDialogArea(dialog ConfirmDialog, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, dialog.View(width),
		lipgloss.WithWhitespaceBackground(styles.TNBg),
		lipgloss.WithWhitespaceForeground(styles.TNBg))
}
