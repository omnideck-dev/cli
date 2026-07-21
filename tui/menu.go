package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
	"github.com/omnideck-dev/cli/styles"
)

// ─── types ────────────────────────────────────────────────────────────────────

type menuScreen int

const (
	menuScreenLauncher menuScreen = iota
	menuScreenAction              // start / stop / status / doctor
	menuScreenLogs
)

type menuActionDoneMsg struct {
	lines []string
	err   error
}

type menuLogsReadyMsg []string

// MenuItem describes one entry in the launcher menu.
type MenuItem struct {
	Key      string
	Label    string
	Desc     string
	External bool // if true, quits and lets caller launch the full TUI
}

// DefaultMenuItems returns the standard set of launcher options.
func DefaultMenuItems() []MenuItem {
	return []MenuItem{
		{Key: "tui", Label: "Open Dashboard", Desc: "Full interactive terminal UI", External: true},
		{Key: "install", Label: "Setup", Desc: "Set up another Omnideck instance", External: true},
		{Key: "update", Label: "Update", Desc: "Pull latest image and restart", External: true},
		{Key: "start", Label: "Start", Desc: "Start the container"},
		{Key: "stop", Label: "Stop", Desc: "Stop the container"},
		{Key: "status", Label: "Status", Desc: "Show current status"},
		{Key: "logs", Label: "Logs", Desc: "Tail container logs"},
		{Key: "doctor", Label: "Doctor", Desc: "Run system health checks"},
	}
}

// MenuModel is the lightweight launcher shown when omnideck is run with no args.
type MenuModel struct {
	version string
	cfg     *config.Config
	eng     engine.Engine
	items   []MenuItem
	cursor  int
	chosen  int // index of chosen External item; -1 = none
	width   int
	height  int

	// action state
	screen      menuScreen
	actionKey   string
	actionTitle string
	loading     bool
	sp          spinner.Model
	resultLines []string
	resultErr   error

	// logs state
	logLines  []string
	logScroll int
}

// NewMenuModel creates the launcher menu model.
func NewMenuModel(version string, cfg *config.Config, eng engine.Engine, items []MenuItem) MenuModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(styles.TNBlue)
	return MenuModel{
		version: version,
		cfg:     cfg,
		eng:     eng,
		items:   items,
		chosen:  -1,
		sp:      sp,
	}
}

// ─── Init / Update ────────────────────────────────────────────────────────────

func (m MenuModel) Init() tea.Cmd { return nil }

func (m MenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case menuActionDoneMsg:
		m.loading = false
		m.resultLines = msg.lines
		m.resultErr = msg.err
		return m, nil

	case menuLogsReadyMsg:
		m.loading = false
		m.logLines = []string(msg)
		visible := m.visibleLogLines()
		if len(m.logLines) > visible {
			m.logScroll = len(m.logLines) - visible
		}
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case menuScreenLauncher:
			return m.handleLauncherKey(msg)
		default:
			return m.handleActionKey(msg)
		}
	}
	return m, nil
}

func (m MenuModel) handleLauncherKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "tab":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter", " ":
		item := m.items[m.cursor]
		if item.External {
			m.chosen = m.cursor
			return m, tea.Quit
		}
		return m.startAction(item.Key)
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m MenuModel) handleActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	switch msg.String() {
	case "esc", "backspace":
		m.screen = menuScreenLauncher
		m.resultLines = nil
		m.resultErr = nil
		m.logLines = nil
		m.logScroll = 0
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "down":
		if m.screen == menuScreenLogs {
			limit := len(m.logLines) - m.visibleLogLines()
			if m.logScroll < limit {
				m.logScroll++
			}
		}
	case "up":
		if m.screen == menuScreenLogs && m.logScroll > 0 {
			m.logScroll--
		}
	case "pgdown", "ctrl+f", "ctrl+v":
		if m.screen == menuScreenLogs {
			visible := m.visibleLogLines()
			limit := len(m.logLines) - visible
			if limit < 0 {
				limit = 0
			}
			m.logScroll = min(m.logScroll+visible, limit)
		}
	case "pgup", "ctrl+b", "alt+v":
		if m.screen == menuScreenLogs {
			m.logScroll = max(m.logScroll-m.visibleLogLines(), 0)
		}
	case "home", "g":
		if m.screen == menuScreenLogs {
			m.logScroll = 0
		}
	case "end", "G":
		if m.screen == menuScreenLogs {
			limit := len(m.logLines) - m.visibleLogLines()
			if limit > 0 {
				m.logScroll = limit
			}
		}
	}
	return m, nil
}

func (m MenuModel) startAction(key string) (MenuModel, tea.Cmd) {
	m.actionKey = key
	m.loading = true
	m.resultLines = nil
	m.resultErr = nil
	m.logLines = nil
	m.logScroll = 0

	if key == "logs" {
		m.screen = menuScreenLogs
		m.actionTitle = "Logs"
	} else {
		m.screen = menuScreenAction
		m.actionTitle = menuTitleFor(key)
	}
	return m, tea.Batch(m.sp.Tick, m.cmdDispatch(key))
}

func menuTitleFor(key string) string {
	switch key {
	case "start":
		return "Start"
	case "stop":
		return "Stop"
	case "status":
		return "Status"
	case "doctor":
		return "Doctor"
	}
	return key
}

// ─── Commands ─────────────────────────────────────────────────────────────────

func (m MenuModel) cmdDispatch(key string) tea.Cmd {
	cfg, eng := m.cfg, m.eng
	switch key {
	case "start":
		return func() tea.Msg { return menuCmdStart(cfg, eng) }
	case "stop":
		return func() tea.Msg { return menuCmdStop(cfg, eng) }
	case "status":
		return func() tea.Msg { return menuCmdStatus(cfg, eng) }
	case "doctor":
		return func() tea.Msg { return menuCmdDoctor(cfg, eng) }
	case "logs":
		return func() tea.Msg { return menuCmdFetchLogs(cfg, eng) }
	}
	return func() tea.Msg {
		return menuActionDoneMsg{err: fmt.Errorf("unknown action: %s", key)}
	}
}

// ─── Operation runners (blocking, run inside tea.Cmd goroutines) ──────────────

func menuCmdStart(cfg *config.Config, eng engine.Engine) menuActionDoneMsg {
	if cfg == nil {
		return menuActionDoneMsg{err: fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")}
	}
	if eng == nil {
		return menuActionDoneMsg{err: fmt.Errorf("No container engine found.\nInstall Podman: https://podman.io/docs/installation")}
	}
	exists, err := eng.ContainerExists(cfg.ContainerName)
	if err != nil {
		return menuActionDoneMsg{err: err}
	}
	if !exists {
		return menuActionDoneMsg{err: fmt.Errorf("container '%s' not found.\nRun: omnideck setup", cfg.ContainerName)}
	}
	if err := eng.StartContainer(cfg.ContainerName); err != nil {
		return menuActionDoneMsg{err: err}
	}
	return menuActionDoneMsg{lines: []string{
		"",
		"  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render(cfg.ContainerName) + "  " + styles.TNFaintText.Render("started"),
	}}
}

func menuCmdStop(cfg *config.Config, eng engine.Engine) menuActionDoneMsg {
	if cfg == nil {
		return menuActionDoneMsg{err: fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")}
	}
	if eng == nil {
		return menuActionDoneMsg{err: fmt.Errorf("No container engine found.\nInstall Podman: https://podman.io/docs/installation")}
	}
	if err := eng.StopContainer(cfg.ContainerName); err != nil {
		s := err.Error()
		if strings.Contains(s, "not running") || strings.Contains(s, "is not running") || strings.Contains(s, "No such container") {
			return menuActionDoneMsg{lines: []string{
				"",
				"  " + styles.TNYellowTxt.Render("!") + "  " + styles.TNTextBold.Render(cfg.ContainerName) + "  " + styles.TNFaintText.Render("already stopped"),
			}}
		}
		return menuActionDoneMsg{err: err}
	}
	return menuActionDoneMsg{lines: []string{
		"",
		"  " + styles.TNGreenTxt.Render("✓") + "  " + styles.TNTextBold.Render(cfg.ContainerName) + "  " + styles.TNFaintText.Render("stopped"),
	}}
}

func menuCmdStatus(cfg *config.Config, eng engine.Engine) menuActionDoneMsg {
	if cfg == nil {
		return menuActionDoneMsg{err: fmt.Errorf("Omnideck is not set up.\nRun: omnideck setup")}
	}
	if eng == nil {
		return menuActionDoneMsg{err: fmt.Errorf("No container engine found.\nInstall Podman: https://podman.io/docs/installation")}
	}

	type gathered struct {
		containerStatus string
		containerErr    error
		ollamaOK        bool
		ollamaHost      string
		homeVolumeOK    bool
		stateVolumeOK   bool
	}
	var g gathered
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		s, err := eng.ContainerStatus(cfg.ContainerName)
		g.containerStatus = s
		g.containerErr = err
	}()
	go func() {
		defer wg.Done()
		g.ollamaOK, g.ollamaHost = checks.CheckOllama()
	}()
	go func() {
		defer wg.Done()
		ok, _ := eng.VolumeExists(cfg.HomeVolumeName())
		g.homeVolumeOK = ok
		ok, _ = eng.VolumeExists(cfg.StateVolumeName())
		g.stateVolumeOK = ok
	}()
	wg.Wait()

	kw := 14
	kv := func(key, val string, valStyle lipgloss.Style) string {
		k := styles.TNTextMid.Render(padRight(key, kw))
		return "  " + k + valStyle.Render(val)
	}

	dot := styles.TNRedTxt.Render("●")
	statusText := styles.TNRedTxt.Render(g.containerStatus)
	if g.containerStatus == "running" {
		dot = styles.TNGreenTxt.Render("●")
		statusText = styles.TNGreenTxt.Render("running")
	}

	ollamaVal := styles.TNRedTxt.Render("✗ not reachable")
	if g.ollamaOK {
		ollamaVal = styles.TNGreenTxt.Render("✓ reachable at " + g.ollamaHost)
	}

	homeVolumeVal := styles.TNRedTxt.Render("✗ not found")
	if g.homeVolumeOK {
		homeVolumeVal = styles.TNGreenTxt.Render("✓ exists")
	}
	stateVolumeVal := styles.TNRedTxt.Render("✗ not found")
	if g.stateVolumeOK {
		stateVolumeVal = styles.TNGreenTxt.Render("✓ exists")
	}

	sep := styles.TNFaintText.Render(strings.Repeat("─", 46))

	lines := []string{
		"",
		"  " + dot + "  " + styles.TNTextBold.Render(cfg.ContainerName) + "  " + statusText,
		"  " + sep,
		"",
		kv("CONTAINER", cfg.ContainerName, styles.TNTextSub),
		kv("IMAGE", cfg.Image, styles.TNCyanTxt),
		kv("RUNTIME", eng.Name(), styles.TNTextSub),
		kv("PORT", ":"+cfg.WebUIPortOrDefault(), styles.TNTextSub),
		kv("HOME VOLUME", cfg.HomeVolumeName()+"  "+homeVolumeVal, styles.TNFaintText),
		kv("STATE VOLUME", cfg.StateVolumeName()+"  "+stateVolumeVal, styles.TNFaintText),
		kv("OLLAMA", ollamaVal, styles.TNFaintText),
		kv("WEB UI", "http://localhost:"+cfg.WebUIPortOrDefault(), styles.TNBlueTxt),
	}
	return menuActionDoneMsg{lines: lines}
}

func menuCmdDoctor(cfg *config.Config, eng engine.Engine) menuActionDoneMsg {
	results := RunDoctorChecks(cfg, eng)
	lines := []string{""}
	for _, r := range results {
		var icon, labelS string
		switch r.Status {
		case CheckPass:
			icon = styles.TNGreenTxt.Render("✓")
			labelS = styles.TNTextMid.Render(r.Label)
		case CheckFail:
			icon = styles.TNRedTxt.Render("✗")
			labelS = styles.TNRedTxt.Render(r.Label)
		case CheckWarn:
			icon = styles.TNYellowTxt.Render("!")
			labelS = styles.TNYellowTxt.Render(r.Label)
		case CheckInfo:
			icon = styles.TNFaintText.Render("·")
			labelS = styles.TNDimText.Render(r.Label)
		}
		detail := styles.TNFaintText.Render(r.Detail)
		lines = append(lines, "  "+icon+"  "+padRightStyled(labelS, 26)+"  "+detail)
		if r.Hint != "" && r.Status != CheckPass {
			lines = append(lines, "       "+styles.TNDimText.Render("→ "+r.Hint))
		}
		if command := doctorActionCommand(r); command != "" {
			lines = append(lines, "       "+styles.TNDimText.Render("Next: "+command))
		}
	}
	return menuActionDoneMsg{lines: lines}
}

func menuCmdFetchLogs(cfg *config.Config, eng engine.Engine) menuLogsReadyMsg {
	if cfg == nil {
		return menuLogsReadyMsg{"  " + styles.TNRedTxt.Render("✗") + "  not set up"}
	}
	if eng == nil {
		return menuLogsReadyMsg{"  " + styles.TNRedTxt.Render("✗") + "  no container engine found — install Podman: https://podman.io/docs/installation"}
	}
	raw, err := eng.FetchLogs(cfg.ContainerName, 200)
	if err != nil {
		return menuLogsReadyMsg{"  " + styles.TNRedTxt.Render("✗") + "  " + err.Error()}
	}
	var out []string
	for _, line := range raw {
		ll := parseLogLine(line)
		num := styles.TNFaintText.Render(fmt.Sprintf("%4d", len(out)+1))
		ts := styles.TNFaintText.Render(padRight(ll.Time, 9))
		lvl := styles.TNLogLevel(ll.Level)
		msg := styles.TNTextMid.Render(ll.Msg)
		out = append(out, "  "+num+"  "+ts+"  "+lvl+"  "+msg)
	}
	return menuLogsReadyMsg(out)
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m MenuModel) View() string {
	switch m.screen {
	case menuScreenAction:
		return m.viewAction()
	case menuScreenLogs:
		return m.viewLogsScreen()
	}
	return m.viewLauncher()
}

// ─── Launcher screen ──────────────────────────────────────────────────────────

func (m MenuModel) viewLauncher() string {
	var b strings.Builder
	b.WriteString(m.menuHeader("Launcher"))
	b.WriteString("\n")

	const labelWidth = 20
	for i, item := range m.items {
		if i == m.cursor {
			cursor := styles.TNBlueTxt.Render(" ❯ ")
			label := lipgloss.NewStyle().Foreground(styles.TNFg).Bold(true).Width(labelWidth).Render(item.Label)
			desc := styles.TNTextMid.Render(item.Desc)
			row := cursor + label + "  " + desc
			b.WriteString(styles.TNSelRow.Width(m.width).Render(row) + "\n")
		} else {
			label := styles.TNTextMid.Width(labelWidth).Render(item.Label)
			desc := styles.TNFaintText.Render(item.Desc)
			b.WriteString("   " + label + "  " + desc + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.menuFooter([][2]string{
		{"↑↓", "move"}, {"enter", "select"}, {"q", "quit"},
	}))
	return b.String()
}

// ─── Action screen ────────────────────────────────────────────────────────────

func (m MenuModel) viewAction() string {
	var b strings.Builder
	b.WriteString(m.menuHeader(m.actionTitle))

	if m.loading {
		b.WriteString("\n\n  " + m.sp.View() + "  " + styles.TNDimText.Render("running…") + "\n")
	} else if m.resultErr != nil {
		b.WriteString("\n\n  " + styles.TNRedTxt.Render("✗") + "  " + styles.TNTextMid.Render(m.resultErr.Error()) + "\n")
	} else {
		for _, l := range m.resultLines {
			b.WriteString(l + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.menuFooter([][2]string{
		{"esc", "back"}, {"q", "quit"},
	}))
	return b.String()
}

// ─── Logs screen ──────────────────────────────────────────────────────────────

func (m MenuModel) viewLogsScreen() string {
	var b strings.Builder

	instName := ""
	if m.cfg != nil {
		instName = m.cfg.ContainerName
	}
	b.WriteString(m.menuHeader("Logs  " + styles.TNFaintText.Render(instName)))

	if m.loading {
		b.WriteString("\n\n  " + m.sp.View() + "  " + styles.TNDimText.Render("fetching logs…") + "\n")
	} else {
		visible := m.visibleLogLines()
		total := len(m.logLines)
		start := m.logScroll
		end := start + visible
		if end > total {
			end = total
		}

		// Title row with scroll position
		scrollInfo := styles.TNFaintText.Render(fmt.Sprintf("%d lines", total))
		if total > visible {
			scrollInfo = styles.TNFaintText.Render(fmt.Sprintf("%d–%d of %d", start+1, end, total))
		}
		titleGap := max(1, m.width-lipgloss.Width(styles.TNDimText.Render("LOGS"))-lipgloss.Width(scrollInfo)-4)
		b.WriteString("\n  " + styles.TNDimText.Render("LOGS") + safeRepeat(" ", titleGap) + scrollInfo + "\n")
		b.WriteString("  " + styles.TNFaintText.Render(safeRepeat("─", m.width-4)) + "\n")

		if total == 0 {
			b.WriteString("\n  " + styles.TNFaintText.Render("no logs available") + "\n")
		} else {
			for _, line := range m.logLines[start:end] {
				b.WriteString(line + "\n")
			}
		}
	}

	footer := [][2]string{{"↑↓", "scroll"}, {"pg↑↓", "page"}, {"home/end", "top/bot"}, {"esc", "back"}, {"q", "quit"}}
	b.WriteString(m.menuFooter(footer))
	return b.String()
}

func (m MenuModel) visibleLogLines() int {
	// header=1 + title+sep=3 + footer=1 + padding=2
	h := m.height - 7
	if h < 1 {
		return 1
	}
	return h
}

// ─── Shared chrome ────────────────────────────────────────────────────────────

func (m MenuModel) menuHeader(breadcrumb string) string {
	logo := styles.TNBoldBlue.Render("◆") + " " + styles.TNTextBold.Render("omnideck")
	sep := styles.TNFaintText.Render(" │ ")
	left := logo + sep + styles.TNDimText.Render(breadcrumb)
	right := styles.TNFaintText.Render("v" + m.version)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return styles.TNHeaderBar.Width(m.width).Render(left+safeRepeat(" ", gap)+right) + "\n"
}

func (m MenuModel) menuFooter(hints [][2]string) string {
	left := keyHints(hints)
	right := styles.TNFaintText.Render("omnideck")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return styles.TNFooterBar.Width(m.width).Render(left + safeRepeat(" ", gap) + right)
}

// ─── Chosen / RunMenu ─────────────────────────────────────────────────────────

// ChosenKey returns the key of the chosen External item, or ("", false) if none.
func (m MenuModel) ChosenKey() (string, bool) {
	if m.chosen < 0 || m.chosen >= len(m.items) {
		return "", false
	}
	return m.items[m.chosen].Key, true
}

// RunMenu runs the launcher menu. Returns the chosen external action key, or
// ("", false) if the user quit or all actions were handled inline.
func RunMenu(version string, cfg *config.Config, eng engine.Engine) (string, bool) {
	m := NewMenuModel(version, cfg, eng, DefaultMenuItems())
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return "", false
	}
	final, ok := result.(MenuModel)
	if !ok {
		return "", false
	}
	return final.ChosenKey()
}
