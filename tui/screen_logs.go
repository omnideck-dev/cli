package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pollLogs returns a command that fetches recent logs for instance idx.
func (m AppModel) pollLogs(idx int) tea.Cmd {
	if m.eng == nil || idx < 0 || idx >= len(m.instances) {
		return nil
	}
	name := m.instances[idx].Info.Config.ContainerName
	eng := m.eng
	return func() tea.Msg {
		raw, err := eng.FetchLogs(name, 200)
		if err != nil {
			return instanceLogsMsg{idx: idx}
		}
		return instanceLogsMsg{idx: idx, lines: parseLogLines(raw)}
	}
}

// filteredLogs returns the logs for the current instance, filtered by logSearchQuery.
func (m AppModel) filteredLogs() []LogLine {
	inst := m.CurrentInstance()
	if inst == nil {
		return nil
	}
	if m.logSearchQuery == "" {
		return inst.Logs
	}
	q := strings.ToLower(m.logSearchQuery)
	var out []LogLine
	for _, ll := range inst.Logs {
		if strings.Contains(strings.ToLower(ll.Time+" "+ll.Level+" "+ll.Msg), q) {
			out = append(out, ll)
		}
	}
	return out
}

// copyLogsCmd returns a command that copies the last 200 (filtered) log lines to the clipboard.
func (m AppModel) copyLogsCmd() tea.Cmd {
	lines := m.filteredLogs()
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	var sb strings.Builder
	for _, ll := range lines {
		if ll.Time != "" {
			sb.WriteString(ll.Time + "  ")
		}
		if ll.Level != "" {
			sb.WriteString("[" + ll.Level + "]  ")
		}
		sb.WriteString(ll.Msg + "\n")
	}
	text := sb.String()
	return func() tea.Msg {
		return logCopyResultMsg{err: copyToClipboard(text)}
	}
}

// --- Log parsing ---

func parseLogLines(raw []string) []LogLine {
	out := make([]LogLine, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, parseLogLine(line))
	}
	return out
}

func parseLogLine(line string) LogLine {
	ts := ""
	rest := line

	// Docker/Podman --timestamps prefix: RFC3339Nano timestamp followed by a space.
	// Podman emits 35-char timestamps like "2026-07-10T18:11:11.207455000-04:00 <rest>".
	// We look for the first space within the first 40 chars and attempt a parse.
	if len(line) > 20 && line[10] == 'T' {
		if idx := strings.IndexByte(line, ' '); idx > 0 && idx <= 40 {
			if t, err := time.Parse(time.RFC3339Nano, line[:idx]); err == nil {
				ts = t.Format("01-02-06 3:04 PM")
				rest = strings.TrimSpace(line[idx+1:])
			}
		}
	}

	// Level is the first word of the trimmed remainder when it is a known keyword.
	trimmed := strings.TrimSpace(rest)
	level := ""
	msg := trimmed
	for _, lvl := range []string{"ERROR", "WARN", "INFO", "READY", "DEBUG"} {
		if strings.HasPrefix(trimmed, lvl) {
			level = lvl
			msg = strings.TrimSpace(trimmed[len(lvl):])
			break
		}
	}

	return LogLine{Time: ts, Level: level, Msg: msg}
}
