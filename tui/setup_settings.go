package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
)

func (m *SetupModel) ensureRecommendedSettingsAvailable() {
	if m.eng != nil {
		candidate := strings.TrimSpace(m.inputs[inputContainerName].Value())
		exists, err := m.eng.ContainerExists(candidate)
		if err == nil && (exists || m.existingNames[candidate]) {
			for suffix := 2; suffix < 10000; suffix++ {
				candidate = "omnideck" + strconv.Itoa(suffix)
				exists, err = m.eng.ContainerExists(candidate)
				if err == nil && !exists && !m.existingNames[candidate] {
					break
				}
			}
		}
		m.inputs[inputContainerName].SetValue(candidate)
	}

	port := strings.TrimSpace(m.inputs[inputWebUIPort].Value())
	if !m.existingPorts[port] && checks.PortAvailable(port) {
		return
	}
	start, err := strconv.Atoi(port)
	if err != nil {
		start = 2337
	}
	if available, ok := checks.NextAvailablePort(start+1, m.existingPorts); ok {
		m.inputs[inputWebUIPort].SetValue(available)
	}
}

func (m SetupModel) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		if !m.settingsAdvanced {
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				return m.exit(WorkflowCanceled)
			case "enter", " ":
				if m.validateAllInputs() {
					m.Stage = SetupStageReview
					m.reviewShowDetails = false
					m.buildReviewWarnings()
				} else {
					m.settingsAdvanced = true
					for i := range m.inputs {
						m.inputs[i].Blur()
					}
					m.inputs[m.inputFocus].Focus()
				}
			case "c":
				m.settingsAdvanced = true
				m.inputFocus = 0
				m.inputs[m.inputFocus].Focus()
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m.exit(WorkflowCanceled)
		case tea.KeyEsc:
			m.inputs[m.inputFocus].Blur()
			m.settingsAdvanced = false
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if m.validateCurrentInput() {
				m.inputs[m.inputFocus].Blur()
				m.inputFocus++
				if m.inputFocus >= inputCount {
					m.inputFocus = inputCount - 1
					m.Stage = SetupStageReview
					m.buildReviewWarnings()
					return m, nil
				}
				m.inputs[m.inputFocus].Focus()
			}
		case tea.KeyShiftTab:
			if m.inputFocus > 0 {
				m.inputs[m.inputFocus].Blur()
				m.inputFocus--
				m.inputs[m.inputFocus].Focus()
			}
		default:
			var cmd tea.Cmd
			m.inputs[m.inputFocus], cmd = m.inputs[m.inputFocus].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *SetupModel) validateCurrentInput() bool {
	val := strings.TrimSpace(m.inputs[m.inputFocus].Value())
	if val == "" {
		m.inputErrs[m.inputFocus] = "cannot be empty"
		return false
	}
	switch m.inputFocus {
	case inputContainerName:
		if !validContainerName(val) {
			m.inputErrs[m.inputFocus] = "only letters, digits, underscores, hyphens, and dots allowed"
			return false
		}
		if m.existingNames[val] {
			m.inputErrs[m.inputFocus] = "an instance named '" + val + "' already exists"
			return false
		}
		if m.eng != nil {
			exists, err := m.eng.ContainerExists(val)
			if err != nil {
				m.inputErrs[m.inputFocus] = "Omnideck could not check whether this name is available"
				return false
			}
			if exists {
				m.inputErrs[m.inputFocus] = "another container already uses this name; choose a different name"
				return false
			}
		}
	case inputMemory, inputShmSize:
		if !validMemSize(val) {
			m.inputErrs[m.inputFocus] = "must be a number + unit (e.g. 512m, 2g)"
			return false
		}
	case inputWebUIPort:
		if !validPort(val) {
			m.inputErrs[m.inputFocus] = "must be a number between 1 and 65535"
			return false
		}
		if m.existingPorts[val] {
			m.inputErrs[m.inputFocus] = "another Omnideck installation already uses this browser address number"
			return false
		}
		if !checks.PortAvailable(val) {
			m.inputErrs[m.inputFocus] = "another app is already using this browser address number"
			return false
		}
	}
	m.inputErrs[m.inputFocus] = ""
	return true
}

func (m *SetupModel) validateAllInputs() bool {
	originalFocus := m.inputFocus
	for i := range m.inputs {
		m.inputFocus = i
		if !m.validateCurrentInput() {
			return false
		}
	}
	m.inputFocus = originalFocus
	return true
}

// expandTilde replaces a leading ~ with the user's home directory for validation.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func validMemSize(s string) bool {
	return checks.ValidMemorySize(s)
}

func validContainerName(s string) bool {
	return checks.ValidContainerName(s)
}

func validPort(s string) bool {
	return checks.ValidPort(s)
}

func (m *SetupModel) buildReviewWarnings() {
	m.reviewWarnings = nil
	if !m.ollamaOK {
		m.reviewWarnings = append(m.reviewWarnings,
			"Optional local AI was not found. That is okay—you can use online AI services now and add Ollama later.")
	}
	if m.memWarning != "" {
		m.reviewWarnings = append(m.reviewWarnings, m.memWarning)
	}
	if m.eng != nil {
		for _, probe := range m.runtimeProbes {
			if probe.Name == m.eng.Name() && probe.Warning != "" {
				m.reviewWarnings = append(m.reviewWarnings, probe.Warning)
			}
		}
	}
}
