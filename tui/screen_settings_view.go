package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// --- Settings screen ---

func (m AppModel) viewSettings() string {
	inst := m.CurrentInstance()
	w := m.width

	contentW := w - 4
	if contentW > 88 {
		contentW = 88
	}
	if contentW < 20 {
		contentW = 20
	}

	var hdrRight string
	if inst != nil {
		hdrRight = styles.TUISubtleText.Render(inst.Info.Name + ".yaml")
	}
	hdrLeft := styles.TUIAccentHoverText.Render("⚙") + "  " + styles.TUIPrimaryBold.Render("Settings")
	hdrGap := contentW - lipgloss.Width(hdrLeft) - lipgloss.Width(hdrRight) - 2
	if hdrGap < 1 {
		hdrGap = 1
	}
	header := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TUISubtleText.Render(safeRepeat("─", contentW))
	if m.settingsStage == settingsStageApplying {
		message := styles.TUIPrimaryBold.Render("Applying settings") + "\n\n" +
			styles.TUISecondaryText.Render("Omnideck is restarting this installation with the new settings. If it cannot start, the previous settings will be restored automatically.")
		body := header + "\n" + sep + "\n\n" + message
		return m.renderScreen(body)
	}

	var rows []string
	for i, f := range m.settingFields {
		selected := i == m.settingFocus
		keyS := styles.TUIPrimaryText
		if selected {
			keyS = styles.TUIAccentText
		}
		typeS := styles.TUISubtleText.Render(f.Type)

		var valStr string
		if selected && m.settingEditing {
			valStr = styles.TUIPrimaryText.Render(m.settingBuffer) + styles.TUIAccentText.Render("█")
		} else {
			valS := styles.TUIPrimaryText
			if f.Changed {
				valS = styles.TUISuccessText
			}
			valStr = valS.Render(tnTruncate(f.Value, contentW-30))
			if f.Changed {
				valStr += "  " + styles.TUISuccessText.Render("●")
			}
		}

		caret := " "
		if selected {
			caret = styles.TUIAccentText.Render("▸")
		}
		row := caret + " " + keyS.Render(padRight(f.Label, 16)) + typeS + "  " + valStr
		rows = append(rows, row)
	}

	legend := styles.TUISuccessText.Render("●") + styles.TUISubtleText.Render(" changed since last save")
	if m.settingsMessage != "" {
		legend = styles.TUIDangerText.Render(m.settingsMessage) + "\n" + legend
	}
	keyhints := styles.TUISubtleText.Render("ctrl+s") + styles.TUISecondaryText.Render("  apply    ") +
		styles.TUISubtleText.Render("esc") + styles.TUISecondaryText.Render("  back")
	body := header + "\n" + sep + "\n" + strings.Join(rows, "\n") + "\n" + sep + "\n" + legend + "\n" + keyhints
	return m.renderScreen(body)
}
