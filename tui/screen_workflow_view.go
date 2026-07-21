package tui

// --- Setup screen ---

func (m AppModel) viewSetup() string {
	contentW := m.width - 4
	if contentW > 96 {
		contentW = 96
	}
	if contentW < 20 {
		contentW = 20
	}

	return m.renderScreen(m.setupModel.TNView(contentW, m.contentHeight()-2))
}

// --- Maintenance screen ---

func (m AppModel) viewMaintenance() string {
	contentW := m.width - 4
	if contentW > 88 {
		contentW = 88
	}
	if contentW < 20 {
		contentW = 20
	}

	return m.renderScreen(m.maintenanceModel.TNView(contentW))
}
