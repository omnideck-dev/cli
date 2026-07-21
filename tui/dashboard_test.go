package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/omnideck-dev/cli/config"
)

func TestSuggestInstallDefaultsForFirstInstance(t *testing.T) {
	cfg := suggestInstallDefaultsFor(nil)
	if cfg.ContainerName != "omnideck" || cfg.WebUIPort != "2337" {
		t.Fatalf("first instance defaults = %s:%s, want omnideck:2337", cfg.ContainerName, cfg.WebUIPort)
	}
}

func TestSuggestInstallDefaultsForAdditionalInstance(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "omnideck", Config: &config.Config{ContainerName: "omnideck", WebUIPort: "2337"}},
		{Name: "omnideck2", Config: &config.Config{ContainerName: "omnideck2", WebUIPort: "2338"}},
	}
	cfg := suggestInstallDefaultsFor(instances)
	if cfg.ContainerName != "omnideck3" || cfg.WebUIPort != "2339" {
		t.Fatalf("additional instance defaults = %s:%s, want omnideck3:2339", cfg.ContainerName, cfg.WebUIPort)
	}
}

func TestInstallScreenUsesFirstRunLanguage(t *testing.T) {
	m := NewDashboardModelForInstall(nil, nil, "", "")
	m.width, m.height = 100, 36
	view := m.viewInstall()

	if !strings.Contains(view, "Setup") {
		t.Fatalf("first-run title missing:\n%s", view)
	}
	if strings.Contains(view, "preflight") || strings.Contains(view, "configure → install") {
		t.Fatalf("install screen exposes internal phase names:\n%s", view)
	}
	if got := m.breadcrumb(); got != "Setup · Quick check" {
		t.Fatalf("first-run breadcrumb = %q, want Setup · Quick check", got)
	}
}

func TestInstallScreenUsesAdditionalInstanceLanguage(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "omnideck", Config: &config.Config{ContainerName: "omnideck", WebUIPort: "2337"}},
	}
	m := NewDashboardModelForInstall(nil, instances, "", "")
	m.width, m.height = 100, 36
	view := m.viewInstall()

	if !strings.Contains(view, "Setup") {
		t.Fatalf("additional-instance title missing:\n%s", view)
	}
	if got := m.breadcrumb(); got != "Setup · Quick check" {
		t.Fatalf("additional-instance breadcrumb = %q, want Setup · Quick check", got)
	}
}

func TestRuntimeRepairUsesSetupHeader(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "omnideck", Config: &config.Config{ContainerName: "omnideck", Engine: "podman", WebUIPort: "2337"}},
	}
	m := NewDashboardModelForRuntimeSetup(instances, "podman")
	m.width, m.height = 100, 36
	view := m.viewInstall()

	if !strings.Contains(view, "Setup") || strings.Contains(view, "Install new instance") {
		t.Fatalf("runtime repair screen has the wrong header:\n%s", view)
	}
}

func TestHeaderUsesPlainInstanceStatusWithoutClock(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []string
		wantLabel string
		wantTone  headerStatusTone
	}{
		{name: "none", wantLabel: "No instances yet", wantTone: headerNeutral},
		{name: "one running", statuses: []string{"running"}, wantLabel: "Omnideck is running", wantTone: headerHealthy},
		{name: "one stopped", statuses: []string{"exited"}, wantLabel: "Omnideck is stopped", wantTone: headerAttention},
		{name: "one unknown", statuses: []string{"unknown"}, wantLabel: "Checking Omnideck…", wantTone: headerNeutral},
		{name: "some running", statuses: []string{"running", "exited", "running"}, wantLabel: "2 of 3 running", wantTone: headerAttention},
		{name: "all running", statuses: []string{"running", "running"}, wantLabel: "2 of 2 running", wantTone: headerHealthy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instances := make([]InstanceState, len(tt.statuses))
			for i, status := range tt.statuses {
				instances[i].Status = status
			}
			label, tone := summarizeInstances(instances)
			if label != tt.wantLabel || tone != tt.wantTone {
				t.Fatalf("summarizeInstances() = %q, %d; want %q, %d", label, tone, tt.wantLabel, tt.wantTone)
			}
		})
	}

	m := NewDashboardModel(nil, nil)
	m.width = 100
	header := m.renderHeader()
	if !strings.Contains(header, "No instances yet") || strings.Contains(header, "13:15:22") {
		t.Fatalf("dashboard header should show useful status without a clock:\n%s", header)
	}

	m.screen = ScreenInstall
	header = m.renderHeader()
	if strings.Contains(header, "No instances yet") || strings.Contains(header, "running") {
		t.Fatalf("setup header should stay focused on setup:\n%s", header)
	}
}

// --- parseLogLine ---

func TestParseLogLineWithTimestamp(t *testing.T) {
	line := "2024-01-15T14:32:01.123456789Z INFO some message here"
	ll := parseLogLine(line)
	if ll.Time != "01-15-24 2:32 PM" {
		t.Errorf("Time: got %q, want '01-15-24 2:32 PM'", ll.Time)
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
