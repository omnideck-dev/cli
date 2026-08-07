package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/engine"
)

func (m SetupModel) updateQuickCheck(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.quickCheckSpinner, cmd = m.quickCheckSpinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if !m.quickCheckReady || m.permChecking {
			if msg.Type == tea.KeyCtrlC || msg.String() == "esc" || msg.String() == "q" {
				return m.exit(WorkflowCanceled)
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m.exit(WorkflowCanceled)
		case "enter", " ":
			if m.permErr != nil {
				return m, nil // block advance if new engine has no permission
			}
			return m.afterRuntimeReady()
		}

	case engineCheckResult:
		m.quickCheckDone++
		m.runtimeProbes = msg.probes
		m.eng = msg.eng
		m.engErr = msg.err
		m.availableEngines = msg.all
		// Run permission check only after engine is known.
		if msg.eng != nil {
			return m, runPermissionCheck(msg.eng)
		}
		// Engine not found — permission check will never fire; count it as done.
		m.quickCheckDone++
		return m, m.maybeAdvanceQuickCheck()

	case permissionCheckResult:
		if m.quickCheckReady {
			// Engine was switched — update result without touching the counter.
			m.permChecking = false
			m.permErr = msg.err
			return m, nil
		}
		m.quickCheckDone++
		m.permErr = msg.err
		return m, m.maybeAdvanceQuickCheck()

	case ollamaCheckResult:
		m.quickCheckDone++
		m.ollamaOK = msg.reachable
		m.ollamaHost = msg.host
		return m, m.maybeAdvanceQuickCheck()

	case memoryCheckResult:
		m.quickCheckDone++
		m.memMB = msg.mb
		m.memWarning = msg.warning
		m.memChecked = true
		return m, m.maybeAdvanceQuickCheck()

	case allQuickCheckDone:
		if m.engErr != nil {
			m.configureRuntimeSetup()
			return m, m.startRuntimeSetup()
		}
		if m.permErr != nil && len(m.availableEngines) <= 1 {
			if m.eng != nil {
				for i := range m.runtimeProbes {
					if m.runtimeProbes[i].Name == m.eng.Name() {
						m.runtimeProbes[i].State = engine.RuntimePermissionDenied
					}
				}
			}
			m.configureRuntimeSetup()
			return m, m.startRuntimeSetup()
		}
		return m.afterRuntimeReady()
	}
	return m, nil
}

func (m *SetupModel) maybeAdvanceQuickCheck() tea.Cmd {
	// We expect: engine(1) + permission(1) + ollama(1) + memory(1) = 4
	if m.quickCheckDone >= 4 {
		return func() tea.Msg { return allQuickCheckDone{} }
	}
	return nil
}

// --- QuickCheck commands ---

func runEngineCheckFor(preferred string) tea.Cmd {
	return func() tea.Msg {
		probes := engine.ProbeAll()
		usable := engine.ReadyEngines(probes)
		if preferred != "" {
			for _, eng := range usable {
				if eng.Name() == preferred {
					return engineCheckResult{eng: eng, all: usable, probes: probes}
				}
			}
			return engineCheckResult{all: usable, probes: probes, err: fmt.Errorf("%s is not ready", preferred)}
		}
		if len(usable) == 0 {
			return engineCheckResult{probes: probes, err: fmt.Errorf("Podman is not ready")}
		}
		return engineCheckResult{eng: readyEngineForSetup(usable, engine.DetectHostPlatform()), all: usable, probes: probes}
	}
}

func readyEngineForSetup(ready []engine.Engine, _ engine.HostPlatform) engine.Engine {
	for _, candidate := range ready {
		if candidate.Name() == "podman" {
			return candidate
		}
	}
	return nil
}

func runPermissionCheck(eng engine.Engine) tea.Cmd {
	return func() tea.Msg {
		if !eng.HasPermission() {
			return permissionCheckResult{err: fmt.Errorf("permission denied")}
		}
		return permissionCheckResult{}
	}
}

func runOllamaCheck() tea.Msg {
	status := checks.CheckOllamaStatus()
	return ollamaCheckResult{
		reachable: status.Running,
		host:      status.Host,
	}
}

func (m SetupModel) windowsPodmanOllamaNeedsSetup() bool {
	return m.isWindowsPodman() && m.ollamaOK && m.ollamaContainerChecked && !m.ollamaContainerOK
}

func (m SetupModel) windowsPodmanOllamaAwaitingCheck() bool {
	return m.isWindowsPodman() && m.ollamaOK && !m.ollamaContainerChecked
}

func (m SetupModel) isWindowsPodman() bool {
	if m.eng == nil || m.eng.Name() != "podman" {
		return false
	}
	hostOS := m.hostPlatform.OS
	if hostOS == "" {
		hostOS = engine.DetectHostPlatform().OS
	}
	return hostOS == "windows"
}

func runMemoryCheck() tea.Msg {
	mb, err := checks.AvailableMemoryMB()
	if err != nil {
		return memoryCheckResult{mb: 0, warning: "could not read memory"}
	}
	return memoryCheckResult{mb: mb, warning: checks.MemoryWarning(mb)}
}
