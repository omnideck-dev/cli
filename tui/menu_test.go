package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/engine"
)

// mockEngine is a minimal Engine stub for testing menu command runners.
type mockEngine struct {
	containerExists bool
	containerStatus string
	startErr        error
	stopErr         error
	fetchLines      []string
	fetchErr        error
}

func (m *mockEngine) Name() string                    { return "mock" }
func (m *mockEngine) IsAvailable() bool               { return true }
func (m *mockEngine) HasPermission() bool              { return true }
func (m *mockEngine) Version() string                  { return "1.0" }
func (m *mockEngine) ImageDigest(string) string        { return "" }
func (m *mockEngine) PullImage(string, chan<- string) error { return nil }
func (m *mockEngine) RunContainer(engine.RunOptions) error { return nil }
func (m *mockEngine) RemoveContainer(string) error     { return nil }
func (m *mockEngine) TailLogs(string, bool, int) error { return nil }
func (m *mockEngine) ContainerExists(string) (bool, error) {
	return m.containerExists, nil
}
func (m *mockEngine) StartContainer(string) error { return m.startErr }
func (m *mockEngine) StopContainer(string) error  { return m.stopErr }
func (m *mockEngine) ContainerStatus(string) (string, error) {
	return m.containerStatus, nil
}
func (m *mockEngine) ContainerStats(string) (string, float64, string, string, float64, error) {
	return "", 0, "", "", 0, nil
}
func (m *mockEngine) FetchLogs(string, int) ([]string, error) {
	return m.fetchLines, m.fetchErr
}
func (m *mockEngine) ContainerInspect(string) (engine.InspectData, error) {
	return engine.InspectData{}, nil
}

var testCfg = &config.Config{
	ContainerName: "omnideck",
	SharedDir:     "/tmp/shared",
	StateDir:      "/tmp/state",
	Image:         "ghcr.io/omnideck-dev/omnideck:main",
	WebUIPort:     "2337",
}

// --- menuCmdStart ---

func TestMenuCmdStartNilCfg(t *testing.T) {
	msg := menuCmdStart(nil, &mockEngine{})
	if msg.err == nil || !strings.Contains(msg.err.Error(), "not installed") {
		t.Errorf("nil cfg: expected 'not installed' error, got %v", msg.err)
	}
}

func TestMenuCmdStartNilEng(t *testing.T) {
	msg := menuCmdStart(testCfg, nil)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "container engine") {
		t.Errorf("nil eng: expected engine error, got %v", msg.err)
	}
}

func TestMenuCmdStartContainerNotFound(t *testing.T) {
	eng := &mockEngine{containerExists: false}
	msg := menuCmdStart(testCfg, eng)
	if msg.err == nil {
		t.Error("expected error when container does not exist")
	}
}

func TestMenuCmdStartSuccess(t *testing.T) {
	eng := &mockEngine{containerExists: true}
	msg := menuCmdStart(testCfg, eng)
	if msg.err != nil {
		t.Errorf("unexpected error: %v", msg.err)
	}
	if len(msg.lines) == 0 {
		t.Error("expected non-empty result lines on success")
	}
}

// --- menuCmdStop ---

func TestMenuCmdStopNilCfg(t *testing.T) {
	msg := menuCmdStop(nil, &mockEngine{})
	if msg.err == nil {
		t.Error("nil cfg should return error")
	}
}

func TestMenuCmdStopNilEng(t *testing.T) {
	msg := menuCmdStop(testCfg, nil)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "container engine") {
		t.Errorf("nil eng: expected engine error, got %v", msg.err)
	}
}

func TestMenuCmdStopAlreadyStopped(t *testing.T) {
	eng := &mockEngine{stopErr: errors.New("container is not running")}
	msg := menuCmdStop(testCfg, eng)
	if msg.err != nil {
		t.Errorf("already-stopped should not be an error, got %v", msg.err)
	}
	combined := strings.Join(msg.lines, " ")
	if !strings.Contains(combined, "already stopped") {
		t.Errorf("expected 'already stopped' in result lines, got %q", combined)
	}
}

func TestMenuCmdStopSuccess(t *testing.T) {
	eng := &mockEngine{}
	msg := menuCmdStop(testCfg, eng)
	if msg.err != nil {
		t.Errorf("unexpected error: %v", msg.err)
	}
	combined := strings.Join(msg.lines, " ")
	if !strings.Contains(combined, "stopped") {
		t.Errorf("expected 'stopped' in result, got %q", combined)
	}
}

// --- menuCmdStatus ---

func TestMenuCmdStatusNilCfg(t *testing.T) {
	msg := menuCmdStatus(nil, &mockEngine{})
	if msg.err == nil {
		t.Error("nil cfg should return error")
	}
}

func TestMenuCmdStatusNilEng(t *testing.T) {
	msg := menuCmdStatus(testCfg, nil)
	if msg.err == nil {
		t.Error("nil eng should return error")
	}
}

func TestMenuCmdStatusRunning(t *testing.T) {
	eng := &mockEngine{containerStatus: "running"}
	msg := menuCmdStatus(testCfg, eng)
	if msg.err != nil {
		t.Errorf("unexpected error: %v", msg.err)
	}
	combined := strings.Join(msg.lines, "\n")
	if !strings.Contains(combined, "running") {
		t.Errorf("expected 'running' in status output, got %q", combined)
	}
}

// --- menuCmdFetchLogs ---

func TestMenuCmdFetchLogsNilCfg(t *testing.T) {
	lines := menuCmdFetchLogs(nil, &mockEngine{})
	if len(lines) == 0 || !strings.Contains(lines[0], "not installed") {
		t.Errorf("nil cfg: expected error line, got %v", lines)
	}
}

func TestMenuCmdFetchLogsNilEng(t *testing.T) {
	lines := menuCmdFetchLogs(testCfg, nil)
	if len(lines) == 0 || !strings.Contains(lines[0], "no container engine") {
		t.Errorf("nil eng: expected error line, got %v", lines)
	}
}

func TestMenuCmdFetchLogsSuccess(t *testing.T) {
	eng := &mockEngine{fetchLines: []string{"INFO startup complete"}}
	lines := menuCmdFetchLogs(testCfg, eng)
	combined := strings.Join([]string(lines), "\n")
	if !strings.Contains(combined, "startup complete") {
		t.Errorf("expected log content in result, got %q", combined)
	}
}

// --- menuTitleFor ---

func TestMenuTitleFor(t *testing.T) {
	cases := map[string]string{
		"start":  "Start",
		"stop":   "Stop",
		"status": "Status",
		"doctor": "Doctor",
	}
	for key, want := range cases {
		got := menuTitleFor(key)
		if got != want {
			t.Errorf("menuTitleFor(%q): got %q, want %q", key, got, want)
		}
	}
}

func TestMenuTitleForUnknown(t *testing.T) {
	got := menuTitleFor("foobar")
	if got != "foobar" {
		t.Errorf("unknown key should pass through, got %q", got)
	}
}

// --- launcher navigation ---

func newTestMenu() MenuModel {
	return NewMenuModel("test", testCfg, nil, DefaultMenuItems())
}

func updateMenu(m MenuModel, msg tea.Msg) MenuModel {
	result, _ := m.Update(msg)
	return result.(MenuModel)
}

func TestMenuLauncherNavigateDown(t *testing.T) {
	m := newTestMenu()
	m = updateMenu(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("after down: cursor should be 1, got %d", m.cursor)
	}
}

func TestMenuLauncherNoWrapAtTop(t *testing.T) {
	m := newTestMenu()
	m = updateMenu(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor should stay at 0 when pressing up at top, got %d", m.cursor)
	}
}

func TestMenuLauncherNoWrapAtBottom(t *testing.T) {
	m := newTestMenu()
	for i := 0; i < 20; i++ {
		m = updateMenu(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	last := len(m.items) - 1
	if m.cursor != last {
		t.Errorf("cursor should stop at last item (%d), got %d", last, m.cursor)
	}
}

func TestMenuLauncherQuit(t *testing.T) {
	m := newTestMenu()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("q key should return tea.Quit command")
	}
}

func TestMenuLauncherExternalItemSetsChosen(t *testing.T) {
	m := newTestMenu()
	// First item (index 0) is "tui" which is External=true.
	m.cursor = 0
	m = updateMenu(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.chosen != 0 {
		t.Errorf("chosen should be 0 after enter on external item, got %d", m.chosen)
	}
	key, ok := m.ChosenKey()
	if !ok {
		t.Fatal("ChosenKey() should return ok=true")
	}
	if key != "tui" {
		t.Errorf("ChosenKey: got %q, want 'tui'", key)
	}
}
