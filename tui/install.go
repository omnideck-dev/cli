package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// --- Preflight result messages ---

type engineCheckResult struct {
	eng    engine.Engine
	all    []engine.Engine
	probes []engine.ProbeResult
	err    error
}

type runtimeCommandDone struct{ err error }

type permissionCheckResult struct{ err error }

type ollamaCheckResult struct {
	reachable bool
	host      string
}

type memoryCheckResult struct {
	mb      int64
	warning string
}

type allPreflightDone struct{}

// --- Install model ---

// InstallModel is the top-level Bubble Tea model for the install wizard.
type InstallModel struct {
	BaseModel

	// Embedded, when true, means this model runs inside DashboardModel.
	// On done/error it emits WizardExitMsg instead of tea.Quit.
	Embedded bool

	// Preflight state.
	eng              engine.Engine
	engErr           error
	availableEngines []engine.Engine // all detected engines (for switching)
	preflightReady   bool            // true when checks pass and user must confirm engine
	permChecking     bool            // true while re-checking permission after engine switch
	permErr          error
	ollamaOK         bool
	ollamaHost       string
	memMB            int64
	memWarning       string
	preflightDone    int // count of completed checks

	// Runtime setup state.
	runtimeProbes       []engine.ProbeResult
	runtimePlans        []engine.SetupPlan
	runtimeChoice       int
	runtimeCommandIndex int
	runtimeBusy         bool
	runtimeConfirm      bool
	runtimeWaiting      bool
	runtimeShowDetails  bool
	runtimeMessage      string
	runtimeLastError    string
	preferredEngine     string

	// Config inputs.
	inputs         []textinput.Model
	inputFocus     int
	inputErrs      []string
	configAdvanced bool

	// Confirm.
	confirmWarnings    []string
	confirmShowDetails bool

	// Install.
	spinnerModel      SpinnerModel
	installErr        error
	lastCompletedStep int             // -1 = none; used for rollback on failure
	existingNames     map[string]bool // container names already in use (cached at init)

	// Image override (from --image flag, not shown in TUI).
	imageOverride string

	// Done.
	configPath string
	savedCfg   *config.Config

	// Error.
	errorMsg         string
	errorDetail      string
	errorShowDetails bool

	preflightSpinner spinner.Model
}

const (
	inputContainerName = iota
	inputMemory
	inputShmSize
	inputWebUIPort
	inputCount
)

// NewInstallModel creates and initialises the install wizard model.
// If initial is non-nil its values are used instead of DefaultConfig().
// imageOverride, if non-empty, replaces the default container image (not shown in TUI).
func NewInstallModel(configPath string, initial *config.Config, imageOverride string) InstallModel {
	inputs := make([]textinput.Model, inputCount)
	defaults := initial
	if defaults == nil {
		defaults = config.DefaultConfig()
	}

	// Calculate memory defaults from total system RAM.
	defaultMem := defaults.Memory
	defaultShm := defaults.ShmSize
	if totalMB, err := checks.TotalMemoryMB(); err == nil && totalMB > 0 {
		defaultMem, defaultShm = checks.DefaultContainerMemory(totalMB)
	}
	if defaultMem == "" {
		defaultMem = "2g"
	}
	if defaultShm == "" {
		defaultShm = "1024m"
	}

	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[inputContainerName].Placeholder = "omnideck"
	inputs[inputContainerName].SetValue(defaults.ContainerName)

	inputs[inputMemory].Placeholder = "2g"
	inputs[inputMemory].SetValue(defaultMem)

	inputs[inputShmSize].Placeholder = "1024m"
	inputs[inputShmSize].SetValue(defaultShm)

	inputs[inputWebUIPort].Placeholder = "2337"
	inputs[inputWebUIPort].SetValue(defaults.WebUIPortOrDefault())

	inputs[inputContainerName].Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	// Cache existing container names to avoid blocking I/O during Update.
	existingNames := map[string]bool{}
	if instances, err := config.ListInstances(); err == nil {
		for _, inst := range instances {
			if inst.Config != nil {
				existingNames[inst.Config.ContainerName] = true
			}
		}
	}

	return InstallModel{
		BaseModel:         BaseModel{Phase: PhasePreflight},
		inputs:            inputs,
		inputErrs:         make([]string, inputCount),
		preflightSpinner:  sp,
		configPath:        configPath,
		lastCompletedStep: -1,
		existingNames:     existingNames,
		imageOverride:     imageOverride,
	}
}

func (m InstallModel) Init() tea.Cmd {
	return tea.Batch(
		m.preflightSpinner.Tick,
		runEngineCheckFor(m.preferredEngine),
		runOllamaCheck,
		runMemoryCheck,
	)
}

func (m InstallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Phase {
	case PhasePreflight:
		return m.updatePreflight(msg)
	case PhaseRuntimeSetup:
		return m.updateRuntimeSetup(msg)
	case PhaseConfig:
		return m.updateConfig(msg)
	case PhaseConfirm:
		return m.updateConfirm(msg)
	case PhaseInstall:
		return m.updateInstall(msg)
	case PhaseDone:
		if isKeyMsg(msg) {
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
	case PhaseError:
		return m.updateError(msg)
	}
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.HandleWindowSize(msg)
	}
	return m, nil
}

func (m InstallModel) updatePreflight(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.preflightSpinner, cmd = m.preflightSpinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if !m.preflightReady || m.permChecking {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		case "enter", " ":
			if m.permErr != nil {
				return m, nil // block advance if new engine has no permission
			}
			m.Phase = PhaseConfig
		case "tab", "s":
			if len(m.availableEngines) < 2 {
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
		m.preflightDone++
		m.eng = msg.eng
		m.engErr = msg.err
		m.availableEngines = msg.all
		m.runtimeProbes = msg.probes
		// Run permission check only after engine is known.
		if msg.eng != nil {
			return m, runPermissionCheck(msg.eng)
		}
		// Engine not found — permission check will never fire; count it as done.
		m.preflightDone++
		return m, m.maybeAdvancePreflight()

	case permissionCheckResult:
		if m.preflightReady {
			// Engine was switched — update result without touching the counter.
			m.permChecking = false
			m.permErr = msg.err
			return m, nil
		}
		m.preflightDone++
		m.permErr = msg.err
		return m, m.maybeAdvancePreflight()

	case ollamaCheckResult:
		m.preflightDone++
		m.ollamaOK = msg.reachable
		m.ollamaHost = msg.host
		return m, m.maybeAdvancePreflight()

	case memoryCheckResult:
		m.preflightDone++
		m.memMB = msg.mb
		m.memWarning = msg.warning
		return m, m.maybeAdvancePreflight()

	case allPreflightDone:
		if m.engErr != nil {
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
			m.configureRuntimeSetup()
			return m, nil
		}
		// Multiple engines available — pause so user can choose before continuing.
		if len(m.availableEngines) > 1 {
			m.preflightReady = true
			return m, nil
		}
		m.Phase = PhaseConfig
		return m, nil
	}
	return m, nil
}

func (m *InstallModel) configureRuntimeSetup() {
	m.Phase = PhaseRuntimeSetup
	m.runtimeConfirm = false
	m.runtimeWaiting = false
	m.runtimeShowDetails = false
	m.runtimeLastError = ""
	m.runtimePlans = engine.BuildSetupPlans(m.runtimeProbes, engine.DetectHostPlatform())
	if m.preferredEngine != "" {
		filtered := m.runtimePlans[:0]
		for _, plan := range m.runtimePlans {
			if plan.Runtime == m.preferredEngine {
				plan.Recommended = true
				plan.Recommendation = "You asked Omnideck to use " + plan.Title + "."
				filtered = append(filtered, plan)
			}
		}
		m.runtimePlans = filtered
	}
	m.runtimeChoice = 0
	for i, plan := range m.runtimePlans {
		if plan.Recommended {
			m.runtimeChoice = i
			break
		}
	}
}

func (m InstallModel) updateRuntimeSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.preflightSpinner, cmd = m.preflightSpinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
		if m.runtimeBusy || len(m.runtimePlans) == 0 {
			return m, nil
		}
		if msg.String() == "esc" {
			if m.runtimeConfirm || m.runtimeWaiting {
				m.runtimeConfirm = false
				m.runtimeWaiting = false
				m.runtimeMessage = ""
				return m, nil
			}
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
		if m.runtimeWaiting {
			switch msg.String() {
			case "r", "enter", " ":
				m.runtimeBusy = true
				m.runtimeMessage = "Checking whether Podman or Docker is ready…"
				return m, runEngineCheckFor(m.preferredEngine)
			case "b":
				m.runtimeWaiting = false
				m.runtimeMessage = ""
			case "o":
				plan := m.runtimePlans[m.runtimeChoice]
				if plan.URL != "" {
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		if m.runtimeConfirm {
			switch msg.String() {
			case "b":
				m.runtimeConfirm = false
				m.runtimeShowDetails = false
			case "d":
				m.runtimeShowDetails = !m.runtimeShowDetails
			case "enter", " ":
				plan := m.runtimePlans[m.runtimeChoice]
				if len(plan.Commands) > 0 {
					m.runtimeBusy = true
					m.runtimeCommandIndex = 0
					m.runtimeMessage = "Starting the first step…"
					return m, execRuntimeCommand(plan.Commands[0])
				}
				if plan.URL != "" {
					m.runtimeConfirm = false
					m.runtimeWaiting = true
					if plan.DirectDownload {
						m.runtimeMessage = "The official Podman installer should now be downloading. Open it from Downloads, finish the installer, return here, and press Enter so Omnideck can check it."
					} else {
						m.runtimeMessage = "Your browser should now be open. Finish the installer there, return here, and press Enter so Omnideck can check it."
					}
					return m, openBrowserCmd(plan.URL)
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "up", "left", "shift+tab":
			m.runtimeChoice = (m.runtimeChoice - 1 + len(m.runtimePlans)) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "down", "right", "tab":
			m.runtimeChoice = (m.runtimeChoice + 1) % len(m.runtimePlans)
			m.runtimeShowDetails = false
			m.runtimeLastError = ""
		case "d":
			m.runtimeShowDetails = !m.runtimeShowDetails
		case "r":
			m.runtimeBusy = true
			m.runtimeMessage = "Checking whether Podman or Docker is ready…"
			return m, runEngineCheckFor(m.preferredEngine)
		case "enter", " ":
			m.runtimeConfirm = true
			m.runtimeShowDetails = false
			m.runtimeMessage = ""
		}
	case runtimeCommandDone:
		if msg.err != nil {
			m.runtimeBusy = false
			m.runtimeConfirm = false
			m.runtimeLastError = msg.err.Error()
			m.runtimeMessage = "That step did not finish. You can try again or choose the other option. Press d if you want to see the technical error."
			return m, nil
		}
		plan := m.runtimePlans[m.runtimeChoice]
		m.runtimeCommandIndex++
		if m.runtimeCommandIndex < len(plan.Commands) {
			command := plan.Commands[m.runtimeCommandIndex]
			m.runtimeMessage = fmt.Sprintf("Starting step %d of %d…", m.runtimeCommandIndex+1, len(plan.Commands))
			return m, execRuntimeCommand(command)
		}
		if plan.RequiresRestart {
			m.runtimeBusy = false
			m.runtimeConfirm = false
			m.runtimeWaiting = true
			m.runtimeMessage = plan.Title + " is starting. Wait until it says it is ready, then return here and press Enter."
			return m, nil
		}
		m.runtimeMessage = "That step finished. Checking that everything is ready…"
		return m, runEngineCheckFor(m.preferredEngine)
	case engineCheckResult:
		m.runtimeBusy = false
		m.runtimeProbes = msg.probes
		if msg.eng != nil {
			m.eng = msg.eng
			m.engErr = nil
			m.availableEngines = msg.all
			m.permErr = nil
			m.Phase = PhaseConfig
			return m, nil
		}
		m.engErr = msg.err
		m.configureRuntimeSetup()
		m.runtimeMessage = "Podman or Docker is not ready yet. Choose an option below, or check again if you just finished an installer."
	}
	return m, nil
}

func execRuntimeCommand(command engine.SetupCommand) tea.Cmd {
	cmd := exec.Command(command.Name, command.Args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return runtimeCommandDone{err: err}
	})
}

// maybeAdvancePreflight returns a Cmd that fires allPreflightDone once all
// 4 checks (engine, permission counted together, ollama, memory) are done.
// Engine check fires permission check as a follow-up, so we wait for 4 total.
func (m *InstallModel) maybeAdvancePreflight() tea.Cmd {
	// We expect: engine(1) + permission(1) + ollama(1) + memory(1) = 4
	if m.preflightDone >= 4 {
		return func() tea.Msg { return allPreflightDone{} }
	}
	return nil
}

func (m InstallModel) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		if !m.configAdvanced {
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				return m, tea.Quit
			case "enter", " ":
				m.Phase = PhaseConfirm
				m.confirmShowDetails = false
				m.buildConfirmWarnings()
			case "c":
				m.configAdvanced = true
				m.inputFocus = 0
				m.inputs[m.inputFocus].Focus()
			}
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.inputs[m.inputFocus].Blur()
			m.configAdvanced = false
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if m.validateCurrentInput() {
				m.inputs[m.inputFocus].Blur()
				m.inputFocus++
				if m.inputFocus >= inputCount {
					m.inputFocus = inputCount - 1
					m.Phase = PhaseConfirm
					m.buildConfirmWarnings()
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

func (m *InstallModel) validateCurrentInput() bool {
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
	}
	m.inputErrs[m.inputFocus] = ""
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

var memSizeRegex = regexp.MustCompile(`^\d+[mMgGkK]$`)

// containerNameRegex matches valid Docker/Podman container names.
var containerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validMemSize(s string) bool {
	return memSizeRegex.MatchString(s)
}

func validContainerName(s string) bool {
	return containerNameRegex.MatchString(s)
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

func (m *InstallModel) buildConfirmWarnings() {
	m.confirmWarnings = nil
	if !m.ollamaOK {
		m.confirmWarnings = append(m.confirmWarnings,
			"Optional local AI was not found. That is okay—you can use online AI services now and add Ollama later.")
	}
	if m.memWarning != "" {
		m.confirmWarnings = append(m.confirmWarnings, m.memWarning)
	}
	if m.eng != nil {
		for _, probe := range m.runtimeProbes {
			if probe.Name == m.eng.Name() && probe.Warning != "" {
				m.confirmWarnings = append(m.confirmWarnings, probe.Warning)
			}
		}
	}
}

func (m InstallModel) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.Phase = PhaseConfig
			m.configAdvanced = false
			m.confirmShowDetails = false
			return m, nil
		case "d":
			m.confirmShowDetails = !m.confirmShowDetails
		case "i", "enter", " ":
			m.Phase = PhaseInstall
			m.lastCompletedStep = -1
			m.errorMsg = ""
			m.errorDetail = ""
			m.errorShowDetails = false
			m.spinnerModel = NewSpinnerModel(installStepLabels, defaultFlavorMessages)
			return m, tea.Batch(m.spinnerModel.Init(), m.startInstallStep(0))
		}
	}
	return m, nil
}

var installStepLabels = []string{
	"Prepare space for your files",
	"Prepare space for Omnideck data",
	"Check for an earlier Omnideck installation",
	"Download Omnideck",
	"Start Omnideck",
	"Remember these settings",
}

func (m InstallModel) updateInstall(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)

	case StepDoneMsg:
		m.lastCompletedStep = msg.Index
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		next := msg.Index + 1
		if next < len(installStepLabels) {
			return m, tea.Batch(cmd, m.startInstallStep(next))
		}
		// All steps done.
		m.Phase = PhaseDone
		return m, cmd

	case StepFailedMsg:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		m.Phase = PhaseError
		m.errorMsg = installStepLabels[msg.Index]
		m.errorDetail = msg.Err.Error()
		m.errorShowDetails = false
		// Attempt rollback: if the container was run (step 4 completed) but
		// config was not saved (step 5 failed), remove the orphaned container.
		if m.lastCompletedStep >= 4 {
			cfg := m.buildConfig()
			eng := m.eng
			rollbackCmd := func() tea.Msg {
				_ = eng.StopContainer(cfg.ContainerName)
				_ = eng.RemoveContainer(cfg.ContainerName)
				return nil
			}
			return m, tea.Batch(cmd, rollbackCmd)
		}
		return m, cmd

	default:
		var cmd tea.Cmd
		m.spinnerModel, cmd = m.spinnerModel.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m InstallModel) updateError(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.HandleWindowSize(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "d":
			m.errorShowDetails = !m.errorShowDetails
		case "r":
			m.Phase = PhaseConfirm
			m.confirmShowDetails = false
		case "b", "enter", "esc", "q", "ctrl+c":
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

// startInstallStep returns the tea.Cmd for step i, sending StepStartedMsg
// immediately and running the work asynchronously.
func (m *InstallModel) startInstallStep(i int) tea.Cmd {
	cfg := m.buildConfig()
	eng := m.eng

	// Fire StepStartedMsg first.
	startCmd := func() tea.Msg { return StepStartedMsg{Index: i} }

	var workCmd tea.Cmd
	switch i {
	case 0: // Create home volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.HomeVolumeName())
		})
	case 1: // Create state volume.
		workCmd = StepCmd(i, func() (string, error) {
			return "", eng.CreateVolume(cfg.StateVolumeName())
		})
	case 2: // Remove existing container.
		workCmd = StepCmd(i, func() (string, error) {
			exists, err := eng.ContainerExists(cfg.ContainerName)
			if err != nil || !exists {
				return "not found, skipped", nil
			}
			return "", eng.RemoveContainer(cfg.ContainerName)
		})
	case 3: // Pull image.
		workCmd = StepCmd(i, func() (string, error) {
			msgs := make(chan string, 32)
			go func() {
				for range msgs {
				}
			}()
			err := eng.PullImage(cfg.Image, msgs)
			close(msgs)
			return "", err
		})
	case 4: // Run container.
		workCmd = StepCmd(i, func() (string, error) {
			opts := engine.RunOptions{
				Name:        cfg.ContainerName,
				Image:       cfg.Image,
				Memory:      cfg.Memory,
				ShmSize:     cfg.ShmSize,
				HomeVolume:  cfg.HomeVolumeName(),
				StateVolume: cfg.StateVolumeName(),
				Restart:     "always",
				WebUIPort:   cfg.WebUIPortOrDefault(),
				Platform:    runtime.GOOS,
			}
			return "", eng.RunContainer(opts)
		})
	case 5: // Save config.
		workCmd = StepCmd(i, func() (string, error) {
			cfg.InstalledAt = time.Now()
			cfg.Engine = eng.Name()
			// Always save to the instances dir keyed by container name.
			savePath := config.InstancePath(cfg.ContainerName)
			m.configPath = savePath
			return savePath, config.Save(savePath, cfg)
		})
	}

	return tea.Sequence(startCmd, workCmd)
}

func (m *InstallModel) buildConfig() *config.Config {
	image := config.DefaultConfig().Image
	if m.imageOverride != "" {
		image = m.imageOverride
	}
	return &config.Config{
		ContainerName: strings.TrimSpace(m.inputs[inputContainerName].Value()),
		Memory:        strings.TrimSpace(m.inputs[inputMemory].Value()),
		ShmSize:       strings.TrimSpace(m.inputs[inputShmSize].Value()),
		WebUIPort:     strings.TrimSpace(m.inputs[inputWebUIPort].Value()),
		Image:         image,
	}
}

// View renders the current wizard phase.
func (m InstallModel) View() string {
	switch m.Phase {
	case PhasePreflight:
		return m.viewPreflight()
	case PhaseRuntimeSetup:
		return m.viewRuntimeSetup()
	case PhaseConfig:
		return m.viewConfig()
	case PhaseConfirm:
		return m.viewConfirm()
	case PhaseInstall:
		return m.viewInstall()
	case PhaseDone:
		return m.viewDone()
	case PhaseError:
		return m.viewError()
	}
	return ""
}

func (m InstallModel) viewPreflight() string {
	out := styles.Header("OMNIDECK", "Quick check", m.WindowWidth)

	type row struct {
		label, detail string
		ok, done      bool
		warn          bool
	}
	var rows []row

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, row{"Podman or Docker", m.eng.Name() + " is ready", true, true, false})
	} else if engDone {
		rows = append(rows, row{"Podman or Docker", "setup needed", false, true, false})
	} else {
		rows = append(rows, row{"Podman or Docker", "", false, false, false})
	}

	if m.eng != nil {
		permDone := m.preflightDone >= 2
		if permDone {
			permOK := m.permErr == nil
			detail := "your account can use it"
			if !permOK {
				detail = "your account needs access"
			}
			rows = append(rows, row{"Account access", detail, permOK, true, false})
		} else {
			rows = append(rows, row{"Account access", "", false, false, false})
		}
	}

	ollamaDone := m.ollamaHost != ""
	ollamaDetail := "not found — you can add it later"
	if m.ollamaOK {
		ollamaDetail = "Ollama is ready"
	}
	rows = append(rows, row{"Local AI (optional)", ollamaDetail, m.ollamaOK, ollamaDone, !m.ollamaOK && ollamaDone})

	memDone := m.memMB > 0
	memDetail := "checking..."
	if memDone {
		memDetail = fmt.Sprintf("%d MB available", m.memMB)
	}
	memWarn := m.memWarning != "" && memDone
	rows = append(rows, row{"Available memory", memDetail, !memWarn, memDone, memWarn})

	rows = append(rows, row{"This computer", friendlyOS(runtime.GOOS), true, true, false})

	const labelW = 22
	for _, r := range rows {
		label := padRight(r.label, labelW)
		if !r.done {
			out += "   " + m.preflightSpinner.View() + "  " +
				styles.Dim.Render(label) +
				styles.Dim.Render("checking...") + "\n"
			continue
		}
		if r.warn {
			out += "   " + styles.WarnMark + "  " + styles.Warning.Render(label) +
				styles.Warning.Render(r.detail) + "\n"
		} else if r.ok {
			out += "   " + styles.CheckMark + "  " + styles.Dim.Render(label) +
				styles.Dim.Render(r.detail) + "\n"
		} else {
			out += "   " + styles.CrossMark + "  " + styles.Error.Render(label) +
				styles.Error.Render(r.detail) + "\n"
		}
	}

	if m.preflightReady {
		out += "\n"
		if m.permChecking {
			out += "   " + m.preflightSpinner.View() + "  " + styles.Dim.Render("Checking permissions for "+m.eng.Name()+"...") + "\n"
		} else if m.permErr != nil {
			out += "   " + styles.CrossMark + "  " + styles.Error.Render("Your account cannot use "+m.eng.Name()+" yet. Choose the other option to continue.") + "\n"
		}
		if len(m.availableEngines) > 1 {
			other := m.availableEngines[0]
			for _, e := range m.availableEngines {
				if e.Name() != m.eng.Name() {
					other = e
					break
				}
			}
			out += "\n   " + styles.Accent.Render("[tab]") + styles.Dim.Render(" switch to "+other.Name()+"    ") +
				styles.Accent.Render("[enter]") + styles.Dim.Render(" continue with "+m.eng.Name()) + "\n"
		} else {
			out += "\n   " + styles.Accent.Render("[enter]") + styles.Dim.Render(" continue") + "\n"
		}
	}
	return out
}

func (m InstallModel) viewConfig() string {
	out := styles.Header("OMNIDECK", "Settings", m.WindowWidth)
	if !m.configAdvanced {
		out += "  " + styles.Active.Render("Recommended settings are ready") + "\n"
		out += "  " + styles.Dim.Render("Omnideck chose sensible settings for this computer. Most people can continue without changing anything.") + "\n\n"
		out += "  " + styles.Dim.Render(padRight("Name", 20)) + styles.Active.Render(m.inputs[inputContainerName].Value()) + "\n"
		out += "  " + styles.Dim.Render(padRight("Open in browser", 20)) + styles.Active.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n"
		out += "  " + styles.Dim.Render(padRight("Memory", 20)) + styles.Active.Render(m.inputs[inputMemory].Value()+" — chosen for this computer") + "\n"
		out += "\n  " + styles.Success.Render("Press Enter to use these settings.") + "\n"
		out += "  " + styles.Dim.Render("Press c to customize them.")
		return out
	}

	fieldNames := []string{"Omnideck name", "Memory limit", "Shared memory", "Browser address number"}
	fieldDescs := []string{
		"A label for this installation. Most people can keep the suggested name.",
		"The most memory Omnideck may use. For example: 2g or 4g.",
		"Temporary working space used by some features. The suggested value is recommended.",
		"The number at the end of the local web address. Change it only if another app already uses it.",
	}

	for i, inp := range m.inputs {
		active := i == m.inputFocus
		label := padRight(fieldNames[i], 20)
		if active {
			out += "\n   " + styles.Accent.Render("▶ ") + styles.Active.Render(label) +
				inp.View() + "\n"
			out += "     " + styles.Dim.Render(fieldDescs[i]) + "\n"
		} else {
			out += "\n   " + styles.Dim.Render("  "+label) + inp.View() + "\n"
		}
		if m.inputErrs[i] != "" {
			out += "     " + styles.Error.Render("✗ "+m.inputErrs[i]) + "\n"
		}
	}

	out += "\n\n   " + styles.Dim.Render("Tab / Enter  ─  next field      Shift+Tab  ─  previous      Esc  ─  recommended settings")
	return out
}

func (m InstallModel) viewConfirm() string {
	out := styles.Header("OMNIDECK", "Ready to install", m.WindowWidth)

	kv := func(k, v string) string {
		return "  " + styles.Dim.Render(padRight(k, 18)) + styles.Active.Render(v) + "\n"
	}

	cfg := m.buildConfig()
	out += "  Here is what Omnideck will do after you press Enter:\n\n"
	out += "    1. Download the Omnideck app.\n"
	out += "    2. Prepare saved space for your files and settings.\n"
	out += "    3. Start Omnideck at http://localhost:" + m.inputs[inputWebUIPort].Value() + ".\n\n"
	out += kv("Runs with", m.eng.Name())
	out += kv("Name", m.inputs[inputContainerName].Value())
	out += kv("Memory", m.inputs[inputMemory].Value())

	for _, w := range m.confirmWarnings {
		out += "\n   " + styles.Warning.Render(w) + "\n"
	}

	if m.confirmShowDetails {
		out += "\n   " + styles.Dim.Render("Technical details") + "\n"
		out += kv("Computer", runtime.GOOS+" / "+runtime.GOARCH)
		out += kv("File storage", cfg.HomeVolumeName())
		out += kv("App storage", cfg.StateVolumeName())
		out += kv("Shared memory", m.inputs[inputShmSize].Value())
		out += kv("Download", cfg.Image)
	}

	out += "\n   " + styles.Accent.Render("[enter]") + styles.Dim.Render(" install    ") +
		styles.Accent.Render("[b]") + styles.Dim.Render(" back    ") +
		styles.Accent.Render("[d]") + styles.Dim.Render(" technical details    ") +
		styles.Accent.Render("[q]") + styles.Dim.Render(" quit")
	return out
}

func (m InstallModel) viewInstall() string {
	out := styles.Header("OMNIDECK", "Installing", m.WindowWidth)
	out += m.spinnerModel.View()
	return out
}

func (m InstallModel) viewDone() string {
	out := "\n" + styles.Success.Render("  ✓  Omnideck installed successfully!") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"

	out += "  " + styles.Dim.Render("Open Omnideck in your browser:") + "\n"
	out += "  " + styles.Active.Render("http://localhost:"+m.inputs[inputWebUIPort].Value()) + "\n\n"
	out += "  " + styles.Dim.Render("Your files and settings will be kept when Omnideck updates.") + "\n"
	out += styles.Dim.Render("  Press any key to exit.")
	return out
}

func (m InstallModel) viewRuntimeSetup() string {
	out := styles.Header("OMNIDECK", "Container setup", m.WindowWidth)
	if len(m.runtimePlans) == 0 {
		return out + "  " + styles.Error.Render("Omnideck could not find a setup option for this computer.")
	}
	plan := m.runtimePlans[m.runtimeChoice]
	if m.runtimeWaiting {
		out += "  " + styles.Active.Render("Finish this step, then come back here") + "\n\n"
		out += "  " + styles.Dim.Render(m.runtimeMessage) + "\n\n"
		out += "  1. Return to this window when the installer, Podman, or Docker says it is ready.\n"
		out += "  2. Press Enter. Omnideck will check everything for you.\n"
		if plan.URL != "" {
			fallback := "If the page did not open, press o to try again or open this address yourself:"
			if plan.DirectDownload {
				fallback = "If the download did not start, press o to try again or open this address yourself:"
			}
			out += "\n  " + fallback + "\n"
			out += "  " + styles.Accent.Render(plan.URL) + "\n"
		}
		hints := "Enter — check again"
		if plan.URL != "" {
			if plan.DirectDownload {
				hints += "    o — download again"
			} else {
				hints += "    o — open page again"
			}
		}
		hints += "    b — back"
		out += "\n  " + styles.Dim.Render(hints) + "\n"
		return out
	}
	if m.runtimeConfirm {
		out += "  " + styles.Dim.Render("STEP 2 OF 2") + "\n"
		out += "  " + styles.Active.Render("Review what will happen") + "\n\n"
		out += "  " + styles.Accent.Render(plan.Action) + "\n"
		out += "  " + styles.Dim.Render(plan.Description) + "\n\n"
		if len(plan.Commands) > 0 {
			out += "  After you press Enter, Omnideck will ask your computer to do these steps:\n"
		} else if plan.DirectDownload {
			out += "  After you press Enter, Omnideck will start the official download. Then you will:\n"
		} else {
			out += "  After you press Enter, Omnideck will open the official page. Then you will:\n"
		}
		for i, step := range plan.Steps {
			out += fmt.Sprintf("    %d. %s\n", i+1, step)
		}
		if plan.PermissionNote != "" {
			out += "\n  " + styles.Warning.Render("About password requests") + "\n"
			out += "  " + styles.Dim.Render(plan.PermissionNote) + "\n"
		}
		if plan.SafetyNote != "" {
			out += "\n  " + styles.Warning.Render("Good to know") + "\n"
			out += "  " + styles.Dim.Render(plan.SafetyNote) + "\n"
		}
		out += "\n  " + styles.Success.Render("Nothing will run until you press Enter.") + "\n"
		if m.runtimeShowDetails {
			out += "\n  " + styles.Dim.Render("Technical details") + "\n"
			for _, command := range plan.Commands {
				out += "    " + styles.Dim.Render("$ "+command.Display) + "\n"
			}
			if plan.URL != "" {
				out += "    " + styles.Dim.Render(plan.URL) + "\n"
			}
		}
		action := "start these steps"
		if len(plan.Commands) == 0 {
			action = "open official page"
			if plan.DirectDownload {
				action = "download installer"
			}
		}
		out += "\n  " + styles.Dim.Render("Enter — "+action+"    b — back    d — technical details") + "\n"
		return out
	}

	out += "  " + styles.Dim.Render("STEP 1 OF 2") + "\n"
	out += "  " + styles.Active.Render("Choose Podman or Docker") + "\n"
	out += "  " + styles.Dim.Render("Omnideck runs inside a container. The container keeps the agent and its software") + "\n"
	out += "  " + styles.Dim.Render("isolated from the rest of your system. Podman or Docker runs the container. You only need one.") + "\n\n"
	out += "  " + styles.Active.Render("What we found") + "\n"
	for _, probe := range m.runtimeProbes {
		name := "Docker"
		if probe.Name == "podman" {
			name = "Podman"
		}
		out += "    " + styles.Dim.Render(padRight(name, 12)+engine.RuntimeStateLabel(probe.State)) + "\n"
	}
	out += "\n  " + styles.Active.Render("Choose how to continue") + "\n"
	for i, option := range m.runtimePlans {
		cursor := "  "
		label := option.Action
		if option.Recommended {
			label += " — Recommended"
		}
		if i == m.runtimeChoice {
			cursor = "▸ "
			label = styles.Active.Render(label)
		} else {
			label = styles.Dim.Render(label)
		}
		out += "  " + styles.Accent.Render(cursor) + label + "\n"
		if i == m.runtimeChoice {
			out += "       " + styles.Dim.Render(option.Description) + "\n"
			if option.Recommendation != "" {
				out += "       " + styles.Success.Render(option.Recommendation) + "\n"
			}
		}
	}
	if m.runtimeMessage != "" {
		out += "\n  " + styles.Dim.Render(m.runtimeMessage) + "\n"
	}
	out += "\n  " + styles.Dim.Render("↑/↓ — choose    Enter — review    d — technical details    r — check again") + "\n"
	return out
}

func (m InstallModel) viewError() string {
	out := "\n" + styles.Error.Render("  ✗  Omnideck could not finish installing") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
	out += "  It stopped while trying to: " + styles.Active.Render(m.errorMsg) + "\n"
	out += "  " + styles.Dim.Render("Any saved space already prepared will be reused if you try again.") + "\n\n"
	out += "  What you can do:\n"
	out += "    • Press r to review the installation and try again.\n"
	out += "    • Press b to return without trying again.\n"
	out += "    • Press d to show or hide details for support.\n"
	if m.errorShowDetails && m.errorDetail != "" {
		out += "\n  " + styles.Dim.Render("Details for support") + "\n"
		out += "  " + styles.Error.Render(m.errorDetail) + "\n"
	}
	return out
}

// isKeyMsg returns true if the message is any key press.
func isKeyMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyMsg)
	return ok
}

// --- Preflight commands ---

func runEngineCheck() tea.Msg {
	return runEngineCheckFor("")()
}

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
		return engineCheckResult{eng: usable[0], all: usable, probes: probes}
	}
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
	ok, host := checks.CheckOllama()
	return ollamaCheckResult{reachable: ok, host: host}
}

func runMemoryCheck() tea.Msg {
	mb, err := checks.AvailableMemoryMB()
	if err != nil {
		return memoryCheckResult{mb: 0, warning: "could not read memory"}
	}
	return memoryCheckResult{mb: mb, warning: checks.MemoryWarning(mb)}
}
