package tui

import tea "github.com/charmbracelet/bubbletea"

func (m AppModel) updateLogs(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Search input mode: characters feed the live filter.
	if m.logSearchMode {
		switch key.Type {
		case tea.KeyEnter: //nolint:exhaustive
			m.logSearchMode = false
		case tea.KeyEsc:
			m.logSearchQuery = ""
			m.logSearchMode = false
			m.logScroll = 0
		case tea.KeyBackspace:
			if len(m.logSearchQuery) > 0 {
				runes := []rune(m.logSearchQuery)
				m.logSearchQuery = string(runes[:len(runes)-1])
				m.logScroll = 0
			}
		default:
			if key.Type == tea.KeyRunes {
				m.logSearchQuery += string(key.Runes)
				m.logScroll = 0
			}
		}
		return m, nil
	}

	// Normal mode.
	filtered := m.filteredLogs()
	totalLines := len(filtered)
	visibleLines := m.logVisibleLines()

	switch key.String() {
	case "esc", "b", "backspace":
		if m.logSearchQuery != "" {
			m.logSearchQuery = ""
			m.logScroll = 0
		} else {
			_, _ = m.router.Back()
		}
	case "q":
		if m.logSearchQuery == "" {
			_, _ = m.router.Back()
		}

	case "down":
		if maxS := totalLines - visibleLines; m.logScroll < maxS {
			m.logScroll++
		}
	case "up":
		if m.logScroll > 0 {
			m.logScroll--
		}
	case "pgdown", "ctrl+f", "ctrl+v":
		maxS := totalLines - visibleLines
		if maxS < 0 {
			maxS = 0
		}
		m.logScroll = min(m.logScroll+visibleLines, maxS)
	case "pgup", "ctrl+b", "alt+v":
		m.logScroll = max(m.logScroll-visibleLines, 0)
	case "home", "g":
		m.logScroll = 0
	case "end", "G":
		if maxS := totalLines - visibleLines; maxS > 0 {
			m.logScroll = maxS
		}

	case "r":
		return m, m.pollLogs(m.selected)
	case "/":
		m.logSearchMode = true
	case "y":
		return m, m.copyLogsCmd()
	}
	return m, nil
}

// logVisibleLines calculates the space remaining below the Logs screen header.
// It must stay in sync with viewLogs().
func (m AppModel) logVisibleLines() int {
	// Full-screen padding plus the header and separator consume four lines.
	visible := m.contentHeight() - 4
	if m.logSearchMode || m.logSearchQuery != "" {
		visible -= 2 // separator + search bar
	}
	if visible < 1 {
		visible = 1
	}
	return visible
}
