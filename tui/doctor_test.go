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
		{"Container", CheckFail, "not found", "Run: omnideck install"},
	}
	report, _ := RenderDoctorReport(results)
	if !strings.Contains(report, "omnideck install") {
		t.Error("hint should appear in report")
	}
}

func TestDirCheckMissing(t *testing.T) {
	r := dirCheck("Test dir", "/nonexistent/path/12345")
	if r.Status != CheckFail {
		t.Error("missing dir should be CheckFail")
	}
}

func TestDirCheckExists(t *testing.T) {
	dir := t.TempDir()
	r := dirCheck("Test dir", dir)
	if r.Status != CheckPass {
		t.Errorf("existing writable dir should be CheckPass, got status %d detail=%q", r.Status, r.Detail)
	}
}
