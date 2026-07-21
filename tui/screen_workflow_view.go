package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// --- Setup screen ---

func (m AppModel) viewSetup() string {
	h := max(8, m.contentHeight()-2)
	w := m.width

	contentW := w - 4
	if contentW > 96 {
		contentW = 96
	}
	if contentW < 20 {
		contentW = 20
	}

	titleRow := styles.TNBoldBlue.Render("◆") + "  " + styles.TNTextBold.Render("Setup")
	sep := styles.TNFaintText.Render(safeRepeat("─", contentW))

	innerW := contentW - 4
	content := titleRow + "\n" + sep + "\n" + m.setupModel.TNView(innerW, h-2)
	return m.renderScreen(content)
}

// --- Maintenance screen ---

func (m AppModel) viewMaintenance() string {
	inst := m.CurrentInstance()
	w := m.width

	contentW := w - 4
	if contentW > 88 {
		contentW = 88
	}
	if contentW < 20 {
		contentW = 20
	}

	instName := ""
	if inst != nil {
		instName = inst.Info.Name
	}
	title := styles.TNBlueTxt.Render("◆") + "  " + styles.TNTextBold.Render(m.maintenanceModel.title()) + "  " + styles.TNFaintText.Render(instName)
	hintText := "review before anything changes"
	switch m.maintenanceModel.Stage {
	case MaintenanceStageApplying:
		hintText = "working — please wait"
	case MaintenanceStageComplete:
		hintText = "press any key to return"
	case MaintenanceStageFailed:
		hintText = "choose retry or return below"
	}
	hint := styles.TNFaintText.Render(hintText)
	gap := contentW - lipgloss.Width(title) - lipgloss.Width(hint) - 2
	titleRow := title + safeRepeat(" ", max(1, gap)) + hint

	updateContent := titleRow + "\n" + m.maintenanceModel.TNView(contentW-4)
	return m.renderScreen(updateContent)
}
