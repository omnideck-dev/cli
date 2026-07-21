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
		hdrRight = styles.TNFaintText.Render(inst.Info.Name + ".yaml")
	}
	hdrLeft := styles.TNPurpleTxt.Render("⚙") + "  " + styles.TNTextBold.Render("Settings")
	hdrGap := contentW - lipgloss.Width(hdrLeft) - lipgloss.Width(hdrRight) - 2
	if hdrGap < 1 {
		hdrGap = 1
	}
	header := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TNFaintText.Render(safeRepeat("─", contentW))
	if m.settingsStage == settingsStageApplying {
		message := styles.TNTextBold.Render("Applying settings") + "\n\n" +
			styles.TNDimText.Render("Omnideck is restarting this installation with the new settings. If it cannot start, the previous settings will be restored automatically.")
		body := header + "\n" + sep + "\n\n" + message
		return m.renderScreen(body)
	}

	var rows []string
	for i, f := range m.settingFields {
		selected := i == m.settingFocus
		keyS := styles.TNTextSub
		if selected {
			keyS = styles.TNBlueTxt
		}
		typeS := styles.TNFaintText.Render(f.Type)

		var valStr string
		if selected && m.settingEditing {
			valStr = styles.TNText.Render(m.settingBuffer) + styles.TNBlueTxt.Render("█")
		} else {
			valS := styles.TNTextSub
			if f.Changed {
				valS = styles.TNGreenTxt
			}
			valStr = valS.Render(tnTruncate(f.Value, contentW-30))
			if f.Changed {
				valStr += "  " + styles.TNGreenTxt.Render("●")
			}
		}

		caret := " "
		if selected {
			caret = styles.TNBlueTxt.Render("▸")
		}
		row := caret + " " + keyS.Render(padRight(f.Label, 16)) + typeS + "  " + valStr
		rows = append(rows, row)
	}

	legend := styles.TNGreenTxt.Render("●") + styles.TNFaintText.Render(" changed since last save")
	if m.settingsMessage != "" {
		legend = styles.TNRedTxt.Render(m.settingsMessage) + "\n" + legend
	}
	keyhints := styles.TNFaintText.Render("ctrl+s") + styles.TNDimText.Render("  apply    ") +
		styles.TNFaintText.Render("esc") + styles.TNDimText.Render("  back")
	body := header + "\n" + sep + "\n" + strings.Join(rows, "\n") + "\n" + sep + "\n" + legend + "\n" + keyhints
	return m.renderScreen(body)
}
