package tui

import (
	"strings"
	"testing"
	"time"
)

// --- parseLogLine ---

func TestParseLogLineWithTimestamp(t *testing.T) {
	line := "2024-01-15T14:32:01.123456789Z INFO some message here"
	ll := parseLogLine(line)
	if ll.Time != "14:32:01" {
		t.Errorf("Time: got %q, want '14:32:01'", ll.Time)
	}
	if ll.Level != "INFO" {
		t.Errorf("Level: got %q, want 'INFO'", ll.Level)
	}
	if ll.Msg != "some message here" {
		t.Errorf("Msg: got %q, want 'some message here'", ll.Msg)
	}
}

func TestParseLogLineWithoutTimestamp(t *testing.T) {
	line := "ERROR connection refused"
	ll := parseLogLine(line)
	if ll.Time != "" {
		t.Errorf("Time should be empty without timestamp prefix, got %q", ll.Time)
	}
	if ll.Level != "ERROR" {
		t.Errorf("Level: got %q, want 'ERROR'", ll.Level)
	}
	if !strings.Contains(ll.Msg, "connection refused") {
		t.Errorf("Msg should contain 'connection refused', got %q", ll.Msg)
	}
}

func TestParseLogLinePlainMessage(t *testing.T) {
	line := "server started on :8080"
	ll := parseLogLine(line)
	if ll.Level != "" {
		t.Errorf("Level should be empty for plain message, got %q", ll.Level)
	}
	if ll.Msg != line {
		t.Errorf("Msg: got %q, want %q", ll.Msg, line)
	}
}

func TestParseLogLineLevels(t *testing.T) {
	for _, lvl := range []string{"ERROR", "WARN", "INFO", "READY", "DEBUG"} {
		line := lvl + " test body"
		ll := parseLogLine(line)
		if ll.Level != lvl {
			t.Errorf("Level: got %q, want %q", ll.Level, lvl)
		}
	}
}

func TestParseLogLinesSkipsEmpty(t *testing.T) {
	raw := []string{"INFO line1", "", "   ", "WARN line2"}
	out := parseLogLines(raw)
	if len(out) != 2 {
		t.Errorf("parseLogLines: expected 2 lines, got %d", len(out))
	}
}

// --- formatDuration ---

func TestFormatDurationSeconds(t *testing.T) {
	got := formatDuration(30 * time.Second)
	if got != "30s" {
		t.Errorf("got %q, want '30s'", got)
	}
}

func TestFormatDurationMinutes(t *testing.T) {
	got := formatDuration(90 * time.Second)
	if got != "1m 30s" {
		t.Errorf("got %q, want '1m 30s'", got)
	}
}

func TestFormatDurationHours(t *testing.T) {
	got := formatDuration(3700 * time.Second) // 1h 1m 40s
	if got != "1h 1m" {
		t.Errorf("got %q, want '1h 1m'", got)
	}
}

func TestFormatDurationDays(t *testing.T) {
	got := formatDuration(25 * time.Hour)
	if got != "1d 1h" {
		t.Errorf("got %q, want '1d 1h'", got)
	}
}

// --- tnTruncate ---

func TestTnTruncateNoTrunc(t *testing.T) {
	got := tnTruncate("hello", 10)
	if got != "hello" {
		t.Errorf("got %q, want 'hello'", got)
	}
}

func TestTnTruncateTruncates(t *testing.T) {
	got := tnTruncate("hello world", 5)
	if got != "hell…" {
		t.Errorf("got %q, want 'hell…'", got)
	}
}

func TestTnTruncateToOne(t *testing.T) {
	got := tnTruncate("hi", 1)
	if got != "…" {
		t.Errorf("got %q, want '…'", got)
	}
}

func TestTnTruncateZeroWidth(t *testing.T) {
	got := tnTruncate("hi", 0)
	if got != "" {
		t.Errorf("got %q, want ''", got)
	}
}

func TestTnTruncateNegativeWidth(t *testing.T) {
	got := tnTruncate("hi", -5)
	if got != "" {
		t.Errorf("got %q, want ''", got)
	}
}

func TestTnTruncateEmpty(t *testing.T) {
	got := tnTruncate("", 5)
	if got != "" {
		t.Errorf("got %q, want ''", got)
	}
}

func TestTnTruncateMultibyteRunes(t *testing.T) {
	// "café" is 4 runes; truncate to 3 should give "ca…"
	got := tnTruncate("café", 3)
	if got != "ca…" {
		t.Errorf("got %q, want 'ca…'", got)
	}
}
