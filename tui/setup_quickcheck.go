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
			if m.quickCheckAlternative != "" {
				m.preferredEngine = m.quickCheckAlternative
				m.runtimeSetupEntry = runtimeSetupFromFirstRunChoice
				m.configureRuntimeSetup()
				return m, nil
			}
			if m.permErr != nil {
				return m, nil // block advance if new engine has no permission
			}
			return m.afterRuntimeReady()
		case "tab", "s":
			if alternative := m.setupAlternativeRuntime(); alternative != "" {
				if m.quickCheckAlternative == alternative {
					m.quickCheckAlternative = ""
				} else {
					m.quickCheckAlternative = alternative
				}
				return m, nil
			}
			if m.preferredEngine != "" || len(m.availableEngines) < 2 {
				return m, nil
			}
			for i, e := range m.availableEngines {
				if e.Name() == m.eng.Name() {
					m.eng = m.availableEngines[(i+1)%len(m.availableEngines)]
					break
				}
			}
			m.permErr = nil
			m.permChecking = true
			return m, runPermissionCheck(m.eng)
		}

	case engineCheckResult:
		m.quickCheckDone++
		m.eng = msg.eng
		m.engErr = msg.err
		m.availableEngines = msg.all
		m.runtimeProbes = msg.probes
		m.quickCheckAlternative = ""
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
			m.runtimeSetupEntry = runtimeSetupFromCheck
			m.configureRuntimeSetup()
			return m, nil
		}
		if m.permErr != nil && len(m.availableEngines) <= 1 {
			if m.eng != nil {
				for i := range m.runtimeProbes {
					if m.runtimeProbes[i].Name == m.eng.Name() {
						m.runtimeProbes[i].State = engine.RuntimePermissionDenied
					}
				}
			}
			m.runtimeSetupEntry = runtimeSetupFromCheck
			m.configureRuntimeSetup()
			return m, nil
		}
		// Only ask the user to choose when both runtimes are installed. A
		// missing alternative is not useful friction on first setup.
		if m.preferredEngine == "" && (len(m.availableEngines) > 1 || m.setupAlternativeRuntime() != "") {
			m.quickCheckReady = true
			return m, nil
		}
		return m.afterRuntimeReady()
	}
	return m, nil
}

// setupAlternativeRuntime returns a second installed runtime that is not ready
// yet. Missing alternatives stay hidden; --runtime is the explicit override.
// Existing instances stay on their shared runtime so saved data is never
// silently stranded.
func (m SetupModel) setupAlternativeRuntime() string {
	if m.setupMode != SetupFirstRun || m.preferredEngine != "" || len(m.existingNames) != 0 || m.eng == nil || len(m.availableEngines) != 1 {
		return ""
	}
	for _, probe := range m.runtimeProbes {
		if probe.Name != m.eng.Name() && probe.State != engine.RuntimeMissing && !probe.Ready() {
			return probe.Name
		}
	}
	return ""
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
			return engineCheckResult{probes: probes, err: fmt.Errorf("neither Podman nor Docker is ready")}
		}
		return engineCheckResult{eng: readyEngineForSetup(usable, engine.DetectHostPlatform()), all: usable, probes: probes}
	}
}

func readyEngineForSetup(ready []engine.Engine, host engine.HostPlatform) engine.Engine {
	recommended := engine.RecommendedRuntime(host)
	for _, candidate := range ready {
		if candidate.Name() == recommended {
			return candidate
		}
	}
	if len(ready) > 0 {
		return ready[0]
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
