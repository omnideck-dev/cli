package tui

import (
	"strings"
	"testing"
)

func TestRenderDoctorReportAllPass(t *testing.T) {
	results := []CheckResult{
		{"Engine", CheckPass, "docker 24.0", ""},
		{"Memory", CheckPass, "8000 MB", ""},
	}
	report, allPass := RenderDoctorReport(results)
	if !allPass {
		t.Error("expected allPass=true")
	}
	if !strings.Contains(report, "Engine") {
		t.Error("report should contain label")
	}
}

func TestRenderDoctorReportWithFail(t *testing.T) {
	results := []CheckResult{
		{"Engine", CheckFail, "not found", "Install Docker"},
		{"Memory", CheckPass, "8000 MB", ""},
	}
	_, allPass := RenderDoctorReport(results)
	if allPass {
		t.Error("expected allPass=false when any check fails")
	}
}

func TestRenderDoctorReportWarnIsNotFail(t *testing.T) {
	results := []CheckResult{
		{"Ollama", CheckWarn, "not reachable", "Install"},
		{"Memory", CheckPass, "8000 MB", ""},
	}
	_, allPass := RenderDoctorReport(results)
	if !allPass {
		t.Error("warnings alone should not fail the report")
	}
}

func TestRenderDoctorReportShowsHint(t *testing.T) {
	results := []CheckResult{
		{"Container", CheckFail, "not found", "Run: omnideck setup"},
	}
	report, _ := RenderDoctorReport(results)
	if !strings.Contains(report, "omnideck setup") {
		t.Error("hint should appear in report")
	}
}

func TestVolumeCheckMissing(t *testing.T) {
	r := volumeCheck("Test volume", "missing-volume", &mockEngine{})
	if r.Status != CheckFail {
		t.Error("missing volume should be CheckFail")
	}
}

func TestVolumeCheckExists(t *testing.T) {
	r := volumeCheck("Test volume", "omnideck-home", &mockEngine{
		volumes: map[string]bool{"omnideck-home": true},
	})
	if r.Status != CheckPass {
		t.Errorf("existing volume should be CheckPass, got status %d detail=%q", r.Status, r.Detail)
	}
}
