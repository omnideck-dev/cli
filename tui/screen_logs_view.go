package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/omnideck-dev/cli/styles"
)

// --- Logs screen ---

func (m AppModel) viewLogs() string {
	inst := m.CurrentInstance()
	w := m.width

	contentW := max(20, w-4)

	// Header row.
	instName := ""
	if inst != nil {
		instName = inst.Info.Name
	}
	filtered := m.filteredLogs()
	var totalLines int
	if inst != nil {
		totalLines = len(inst.Logs)
	}

	hdrLeft := styles.TUIPrimaryBold.Render(instName) + " " + styles.TUISubtleText.Render("stdout + stderr")
	var hdrRight string
	switch {
	case m.logCopied:
		hdrRight = styles.TUISuccessText.Render("✓ copied!")
	case m.logSearchQuery != "":
		hdrRight = styles.TUIAccentText.Render(fmt.Sprintf("%d", len(filtered))) +
			styles.TUISubtleText.Render(fmt.Sprintf(" of %d", totalLines))
	default:
		hdrRight = styles.TUISubtleText.Render(fmt.Sprintf("%d lines", totalLines))
	}
	hdrRight += "  " + styles.TUISubtleText.Render("following")

	hdrGap := max(1, contentW-lipgloss.Width(hdrLeft)-lipgloss.Width(hdrRight)-2)
	hdrLine := hdrLeft + safeRepeat(" ", hdrGap) + hdrRight
	sep := styles.TUISubtleText.Render(safeRepeat("─", contentW))

	// Log lines with word-wrap. prefix = "  " + num(4) + "  " + ts(9) + "  " + lvl(5) + "  " = 26
	// logViewPrefixW: "  "(2) + num(4) + "  "(2) + ts(19) + "  "(2) + lvl(5) + "  "(2) = 36
	const logViewPrefixW = 36
	msgW := contentW - logViewPrefixW
	if msgW < 10 {
		msgW = 10
	}
	msgContW := msgW - 2
	if msgContW < 4 {
		msgContW = 4
	}
	logContIndent := safeRepeat(" ", logViewPrefixW+2)

	visibleH := m.logVisibleLines()
	start := m.logScroll
	if start < 0 {
		start = 0
	}

	var logSb strings.Builder
	rendered := 0
	for entryNum, ll := range filtered[start:] {
		if rendered >= visibleH {
			break
		}
		// Blank line before labeled entries to visually group them — skip for the first visible line.
		if ll.Level != "" && entryNum > 0 && rendered < visibleH {
			logSb.WriteString("\n")
			rendered++
		}
		if rendered >= visibleH {
			break
		}
		num := styles.TUISubtleText.Render(fmt.Sprintf("%4d", start+entryNum+1))
		ts := styles.TUISubtleText.Render(padRight(ll.Time, 19))
		lvl := styles.TUILogLevel(ll.Level)
		prefix := "  " + num + "  " + ts + "  " + lvl + "  "
		for i, part := range wrapWords(ll.Msg, msgW, msgContW) {
			if rendered >= visibleH {
				break
			}
			if i == 0 {
				logSb.WriteString(prefix + styles.TUISecondaryText.Render(part) + "\n")
			} else {
				logSb.WriteString(logContIndent + styles.TUISecondaryText.Render(part) + "\n")
			}
			rendered++
		}
	}
	if start >= len(filtered) {
		if m.logSearchQuery != "" {
			logSb.WriteString("  " + styles.TUISubtleText.Render("no matching lines") + "\n")
		} else {
			logSb.WriteString("  " + styles.TUISubtleText.Render("no logs available") + "\n")
		}
	}

	body := hdrLine + "\n" + sep + "\n" + logSb.String()

	// Search bar below log content.
	if m.logSearchMode || m.logSearchQuery != "" {
		prefix := styles.TUIAccentText.Render("/  ")
		queryStr := m.logSearchQuery
		if m.logSearchMode {
			queryStr += "█"
		}
		var searchRight string
		if m.logSearchQuery != "" {
			searchRight = styles.TUISubtleText.Render(fmt.Sprintf("%d of %d", len(filtered), totalLines))
		} else {
			searchRight = styles.TUISubtleText.Render("type to filter…")
		}
		queryRender := styles.TUIPrimaryText.Render(queryStr)
		searchGap := max(1, contentW-lipgloss.Width(prefix)-lipgloss.Width(queryRender)-lipgloss.Width(searchRight)-2)
		searchLine := sep + "\n" + prefix + queryRender + safeRepeat(" ", searchGap) + searchRight
		body += searchLine
	}

	return m.renderScreen(body)
}
