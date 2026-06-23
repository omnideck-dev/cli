package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/styles"
)

// PickerModel is a minimal Bubble Tea program for choosing one of several instances.
type PickerModel struct {
	title       string
	subtitle    string
	items       []config.InstanceInfo
	cursor      int
	chosen      int // index into items, -1 = cancelled
	windowWidth int
}

// NewPickerModel creates a picker for the given instances.
func NewPickerModel(title, subtitle string, items []config.InstanceInfo) PickerModel {
	return PickerModel{
		title:    title,
		subtitle: subtitle,
		items:    items,
		chosen:   -1,
	}
}

func (m PickerModel) Init() tea.Cmd { return nil }

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
	case tea.KeyMsg:
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
			m.chosen = m.cursor
			return m, tea.Quit
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m PickerModel) View() string {
	out := "\n"
	out += styles.Title.Render("  "+m.title) + "\n"
	if m.subtitle != "" {
		out += styles.Dim.Render("  "+m.subtitle) + "\n"
	}
	out += "\n"

	for i, item := range m.items {
		cursor := "   "
		nameStyle := styles.Dim
		if i == m.cursor {
			cursor = styles.Active.Render(" ❯ ")
			nameStyle = styles.Active
		}

		detail := ""
		if item.Config != nil {
			detail = styles.Dim.Render(fmt.Sprintf("  %s · %s", item.Config.Engine, item.Config.ContainerName))
		}
		out += cursor + nameStyle.Render(item.Name) + detail + "\n"
	}

	out += "\n" + styles.Dim.Render("  ↑↓  navigate    Enter  select    q  cancel") + "\n"
	return out
}

// Chosen returns the selected InstanceInfo, or (zero, false) if cancelled.
func (m PickerModel) Chosen() (config.InstanceInfo, bool) {
	if m.chosen < 0 || m.chosen >= len(m.items) {
		return config.InstanceInfo{}, false
	}
	return m.items[m.chosen], true
}

// RunPicker runs the picker as a full-screen Bubble Tea program and returns
// the chosen instance, or (zero, false) if the user cancelled.
func RunPicker(title, subtitle string, items []config.InstanceInfo) (config.InstanceInfo, bool) {
	m := NewPickerModel(title, subtitle, items)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return config.InstanceInfo{}, false
	}
	final, ok := result.(PickerModel)
	if !ok {
		return config.InstanceInfo{}, false
	}
	return final.Chosen()
}
