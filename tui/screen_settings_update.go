package tui

import tea "github.com/charmbracelet/bubbletea"

func (m AppModel) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settingsStage == settingsStageApplying {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.settingEditing {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			m.settingFields[m.settingFocus].Value = m.settingBuffer
			m.settingFields[m.settingFocus].Changed = (m.settingBuffer != m.settingFields[m.settingFocus].Orig)
			m.settingEditing = false
			m.settingBuffer = ""
		case tea.KeyEsc:
			m.settingEditing = false
			m.settingBuffer = ""
		case tea.KeyBackspace:
			if len(m.settingBuffer) > 0 {
				m.settingBuffer = m.settingBuffer[:len(m.settingBuffer)-1]
			}
		default:
			if key.Type == tea.KeyRunes {
				m.settingBuffer += string(key.Runes)
			}
		}
		return m, nil
	}

	switch key.String() {
	case "esc", "b", "q", "backspace":
		if m.hasSettingChanges() {
			dialog := NewDiscardSettingsDialog()
			m.dialog = &dialog
			return m, nil
		}
		_, _ = m.router.Back()
		m.settingFields = nil

	case "ctrl+s":
		inst := m.CurrentInstance()
		if inst == nil {
			break
		}
		needsRestart := false
		for _, f := range m.settingFields {
			if !f.Changed {
				continue
			}
			switch f.Key {
			case "home_volume", "state_volume", "memory", "shm_size", "web_ui_port", "image":
				needsRestart = true
			}
		}
		if err := m.validateSettingFields(); err != nil {
			m.settingsMessage = "Cannot apply these settings: " + err.Error()
			return m, nil
		}
		m.settingsMessage = ""
		candidate := m.configFromSettingFields()
		if needsRestart {
			m.toast = "Applying settings…"
			m.settingsStage = settingsStageApplying
			return m, applySettingsCmd(inst.Info.Config, candidate, inst.Info.Path, m.eng, m.selected)
		}
		m.toast = "No settings changed"
		return m, clearToastCmd()

	case "down", "tab":
		if m.settingFocus < len(m.settingFields)-1 {
			m.settingFocus++
		}

	case "up", "shift+tab":
		if m.settingFocus > 0 {
			m.settingFocus--
		}

	case "enter":
		if len(m.settingFields) > 0 {
			m.settingBuffer = m.settingFields[m.settingFocus].Value
			m.settingEditing = true
		}
	}
	return m, nil
}

func (m AppModel) hasSettingChanges() bool {
	for _, field := range m.settingFields {
		if field.Changed {
			return true
		}
	}
	return false
}
