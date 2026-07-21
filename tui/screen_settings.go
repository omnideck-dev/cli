package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/workflow"
)

// buildSettingFields populates m.settingFields from the current instance's config.
func (m *AppModel) buildSettingFields() {
	inst := m.CurrentInstance()
	if inst == nil {
		m.settingFields = nil
		return
	}
	cfg := inst.Info.Config
	m.settingFields = []settingField{
		{Key: "home_volume", Label: "File storage", Type: "name", Value: cfg.HomeVolumeName(), Orig: cfg.HomeVolumeName()},
		{Key: "state_volume", Label: "App storage", Type: "name", Value: cfg.StateVolumeName(), Orig: cfg.StateVolumeName()},
		{Key: "memory", Label: "Memory limit", Type: "size", Value: cfg.Memory, Orig: cfg.Memory},
		{Key: "shm_size", Label: "Shared memory", Type: "size", Value: cfg.ShmSize, Orig: cfg.ShmSize},
		{Key: "web_ui_port", Label: "Browser port", Type: "number", Value: cfg.WebUIPortOrDefault(), Orig: cfg.WebUIPortOrDefault()},
		{Key: "image", Label: "Container image", Type: "advanced", Value: cfg.Image, Orig: cfg.Image},
	}
	m.settingFocus = 0
	m.settingsStage = settingsStageEditing
	m.settingsMessage = ""
}

// configFromSettingFields returns a candidate configuration without mutating or
// persisting the configuration that describes the currently running container.
func (m *AppModel) configFromSettingFields() *config.Config {
	inst := m.CurrentInstance()
	if inst == nil {
		return nil
	}
	candidate := *inst.Info.Config
	for _, f := range m.settingFields {
		if !f.Changed {
			continue
		}
		_ = workflow.ApplySetting(&candidate, f.Key, f.Value)
	}
	return &candidate
}

func (m *AppModel) validateSettingFields() error {
	inst := m.CurrentInstance()
	if inst == nil || inst.Info.Config == nil {
		return nil
	}
	oldPort := inst.Info.Config.WebUIPortOrDefault()
	candidate := *inst.Info.Config
	for _, field := range m.settingFields {
		if !field.Changed {
			continue
		}
		if err := workflow.ApplySetting(&candidate, field.Key, field.Value); err != nil {
			return err
		}
		switch field.Key {
		case "web_ui_port":
			if !checks.ValidPort(field.Value) {
				return fmt.Errorf("browser address number must be between 1 and 65535")
			}
			for i, other := range m.instances {
				if i != m.selected && other.Info.Config != nil && other.Info.Config.WebUIPortOrDefault() == field.Value {
					return fmt.Errorf("another Omnideck installation already uses browser address number %s", field.Value)
				}
			}
			if field.Value != oldPort && !checks.PortAvailable(field.Value) {
				return fmt.Errorf("another app is already using browser address number %s", field.Value)
			}
		}
	}
	return nil
}

// applySettingsCmd recreates the container first, then saves the candidate settings.
// If either operation fails it restores the previous runtime/settings pairing.
func applySettingsCmd(current, candidate *config.Config, configPath string, eng engine.Engine, idx int) tea.Cmd {
	return func() tea.Msg {
		currentCopy := *current
		candidateCopy := *candidate
		if candidateCopy.WebUIPortOrDefault() != currentCopy.WebUIPortOrDefault() && !checks.PortAvailable(candidateCopy.WebUIPortOrDefault()) {
			return settingsApplyDoneMsg{err: fmt.Errorf("another app is already using browser address number %s", candidateCopy.WebUIPortOrDefault()), idx: idx}
		}
		if err := workflow.RecreateAndSave(eng, &currentCopy, &candidateCopy, configPath); err != nil {
			return settingsApplyDoneMsg{err: err, idx: idx}
		}
		return settingsApplyDoneMsg{cfg: &candidateCopy, idx: idx}
	}
}
