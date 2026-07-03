package tui

import (
	"fmt"
	"os"
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
	eng engine.Engine
	all []engine.Engine
	err error
}

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

	// Config inputs.
	inputs     []textinput.Model
	inputFocus int
	inputErrs  []string

	// Confirm.
	confirmWarnings []string

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
	errorMsg      string
	noEngineFound bool // true when neither Docker nor Podman was detected

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
		runEngineCheck,
		runOllamaCheck,
		runMemoryCheck,
	)
}

func (m InstallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Phase {
	case PhasePreflight:
		return m.updatePreflight(msg)
	case PhaseConfig:
		return m.updateConfig(msg)
	case PhaseConfirm:
		return m.updateConfirm(msg)
	case PhaseInstall:
		return m.updateInstall(msg)
	case PhaseDone, PhaseError:
		if isKeyMsg(msg) {
			if m.Embedded {
				return m, func() tea.Msg { return WizardExitMsg{} }
			}
			return m, tea.Quit
		}
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
			m.Phase = PhaseError
			m.noEngineFound = true
			return m, nil
		}
		if m.permErr != nil && len(m.availableEngines) <= 1 {
			engName := "docker"
			if m.eng != nil {
				engName = m.eng.Name()
			}
			m.Phase = PhaseError
			m.errorMsg = "Error: " + engName + " found but permission denied.\nFix: sudo usermod -aG " + engName + " $USER\nThen log out and back in."
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
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
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
			fmt.Sprintf("⚠  Ollama not found at %s\n   Install: https://ollama.com\n   You can install Omnideck now and start Ollama later.", m.ollamaHost))
	}
	if m.memWarning != "" {
		m.confirmWarnings = append(m.confirmWarnings, "⚠  "+m.memWarning)
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
			m.inputFocus = inputCount - 1
			m.inputs[m.inputFocus].Focus()
			return m, nil
		case "i":
			m.Phase = PhaseInstall
			m.spinnerModel = NewSpinnerModel(installStepLabels, defaultFlavorMessages)
			return m, tea.Batch(m.spinnerModel.Init(), m.startInstallStep(0))
		}
	}
	return m, nil
}

var installStepLabels = []string{
	"Create home volume",
	"Create state volume",
	"Remove existing container (if present)",
	"Pull image",
	"Run container",
	"Save configuration",
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
		m.errorMsg = fmt.Sprintf("Step '%s' failed: %v", installStepLabels[msg.Index], msg.Err)
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
	case PhaseConfig:
		return m.viewConfig()
	case PhaseConfirm:
		return m.viewConfirm()
	case PhaseInstall:
		return m.viewInstall()
	case PhaseDone:
		return m.viewDone()
	case PhaseError:
		if m.noEngineFound {
			return m.viewNoEngine()
		}
		return m.viewError()
	}
	return ""
}

func (m InstallModel) viewPreflight() string {
	out := styles.Header("OMNIDECK", "Preflight", m.WindowWidth)

	type row struct {
		label, detail string
		ok, done      bool
		warn          bool
	}
	var rows []row

	engDone := m.eng != nil || m.engErr != nil
	if engDone && m.eng != nil {
		rows = append(rows, row{"Container engine", m.eng.Name(), true, true, false})
	} else if engDone {
		rows = append(rows, row{"Container engine", "not found", false, true, false})
	} else {
		rows = append(rows, row{"Container engine", "", false, false, false})
	}

	permDone := m.preflightDone >= 2
	if permDone {
		permOK := m.permErr == nil
		detail := "ok"
		if !permOK {
			detail = "permission denied"
		}
		rows = append(rows, row{"Engine permissions", detail, permOK, true, false})
	} else if m.eng != nil {
		rows = append(rows, row{"Engine permissions", "", false, false, false})
	}

	ollamaDone := m.ollamaHost != ""
	ollamaDetail := "not found — install later from https://ollama.com"
	if m.ollamaOK {
		ollamaDetail = "reachable at " + m.ollamaHost
	}
	rows = append(rows, row{"Ollama  :11434", ollamaDetail, m.ollamaOK, ollamaDone, !m.ollamaOK && ollamaDone})

	memDone := m.memMB > 0
	memDetail := "checking..."
	if memDone {
		memDetail = fmt.Sprintf("%d MB available", m.memMB)
	}
	memWarn := m.memWarning != "" && memDone
	rows = append(rows, row{"Memory", memDetail, !memWarn, memDone, memWarn})

	rows = append(rows, row{"OS / Arch", runtime.GOOS + " / " + runtime.GOARCH, true, true, false})

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
			engName := m.eng.Name()
			out += "   " + styles.CrossMark + "  " + styles.Error.Render(engName+" permission denied — switch engine or fix permissions") + "\n"
			out += "   " + styles.Dim.Render("     Fix: sudo usermod -aG "+engName+" $USER  (then log out and back in)") + "\n"
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
	out := styles.Header("OMNIDECK", "Configuration", m.WindowWidth)

	fieldNames := []string{"Container name", "Memory limit", "SHM size", "Web UI port"}
	fieldDescs := []string{
		"name for the Docker/Podman container",
		"container RAM limit  (e.g. 2g, 4g) — default based on your system RAM",
		"shared memory size  (e.g. 512m, 1g) — default is 50% of memory limit",
		"host port for the web UI  (e.g. 2337, 2338 for a second instance)",
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

	out += "\n\n   " + styles.Dim.Render("Tab / Enter  ─  next field      Shift+Tab  ─  previous      Esc  ─  quit")
	return out
}

func (m InstallModel) viewConfirm() string {
	out := styles.Header("OMNIDECK", "Confirm", m.WindowWidth)

	kv := func(k, v string) string {
		return "  " + styles.Dim.Render(padRight(k, 18)) + styles.Active.Render(v) + "\n"
	}

	image := config.DefaultConfig().Image
	if m.imageOverride != "" {
		image = m.imageOverride
	}
	cfg := m.buildConfig()
	summary := kv("Engine", m.eng.Name()) +
		kv("OS / Arch", runtime.GOOS+" / "+runtime.GOARCH) +
		"\n" +
		kv("Container name", m.inputs[inputContainerName].Value()) +
		kv("Home volume", cfg.HomeVolumeName()) +
		kv("State volume", cfg.StateVolumeName()) +
		kv("Memory limit", m.inputs[inputMemory].Value()) +
		kv("SHM size", m.inputs[inputShmSize].Value()) +
		kv("Web UI port", m.inputs[inputWebUIPort].Value()) +
		kv("Image", image)

	out += styles.Border.Render(summary) + "\n"

	for _, w := range m.confirmWarnings {
		out += "\n   " + styles.Warning.Render(w) + "\n"
	}

	out += "\n   " + styles.Accent.Render("[i]") + styles.Dim.Render(" install    ") +
		styles.Accent.Render("[b]") + styles.Dim.Render(" back    ") +
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

	kv := func(k, v string) string {
		return "  " + styles.Dim.Render(padRight(k, 14)) + styles.Active.Render(v) + "\n"
	}
	out += kv("Web UI", "http://localhost:"+m.inputs[inputWebUIPort].Value())
	out += kv("Ollama", checks.OllamaHost())
	cfg := m.buildConfig()
	out += kv("Home volume", cfg.HomeVolumeName())
	out += kv("State volume", cfg.StateVolumeName())
	out += kv("Config", m.configPath)
	out += "\n" + styles.Dim.Render("  Press any key to exit.")
	return out
}

func (m InstallModel) viewNoEngine() string {
	out := "\n" + styles.Error.Render("  ✗  No container engine found") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
	out += "  " + styles.Active.Render("Omnideck requires Docker or Podman to run containers.") + "\n\n"

	divider := styles.Dim.Render("  ─────────────────────────────────────") + "\n"

	if runtime.GOOS == "darwin" {
		out += divider
		out += "  " + styles.Accent.Render("Docker Desktop") + styles.Dim.Render("  (recommended)") + "\n"
		out += "  " + styles.Dim.Render("https://www.docker.com/products/docker-desktop/") + "\n\n"
		out += "  " + styles.Accent.Render("Podman Desktop") + "\n"
		out += "  " + styles.Dim.Render("https://podman-desktop.io") + "\n"
		out += divider
	} else {
		out += divider
		out += "  " + styles.Accent.Render("Docker") + "\n"
		out += "  " + styles.Dim.Render("sudo apt install docker.io   # Debian / Ubuntu") + "\n"
		out += "  " + styles.Dim.Render("sudo dnf install docker      # Fedora / RHEL") + "\n\n"
		out += "  " + styles.Accent.Render("Podman") + styles.Dim.Render("  (rootless, no daemon required)") + "\n"
		out += "  " + styles.Dim.Render("sudo apt install podman      # Debian / Ubuntu") + "\n"
		out += "  " + styles.Dim.Render("sudo dnf install podman      # Fedora / RHEL") + "\n"
		out += divider
	}

	out += "\n  " + styles.Dim.Render("After installing, restart your terminal and run:") + "\n"
	out += "  " + styles.Active.Render("omnideck install") + "\n"
	out += "\n" + styles.Dim.Render("  Press any key to exit.")
	return out
}

func (m InstallModel) viewError() string {
	out := "\n" + styles.Error.Render("  ✗  Installation failed") + "\n"
	out += styles.Dim.Render("  ─────────────────────────────────────") + "\n\n"
	out += styles.Error.Render("  "+m.errorMsg) + "\n"
	out += "\n" + styles.Dim.Render("  Press any key to exit.")
	return out
}

// isKeyMsg returns true if the message is any key press.
func isKeyMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyMsg)
	return ok
}

// --- Preflight commands ---

func runEngineCheck() tea.Msg {
	all := engine.DetectAll()
	if len(all) == 0 {
		return engineCheckResult{err: fmt.Errorf("neither Podman nor Docker was found")}
	}
	return engineCheckResult{eng: all[0], all: all}
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
