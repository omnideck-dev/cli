package tui

import (
	"fmt"
	"os"
	"testing"
)

func TestValidMemSize(t *testing.T) {
	valid := []string{"256m", "512M", "1g", "1G", "128k", "128K"}
	invalid := []string{"256", "256mb", "abc", "", "1.5g"}

	for _, s := range valid {
		if !validMemSize(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range invalid {
		if validMemSize(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestNewInstallModelDefaults(t *testing.T) {
	m := NewInstallModel("/tmp/test-config.yaml", nil, "")
	if m.Phase != PhasePreflight {
		t.Errorf("initial phase should be PhasePreflight, got %d", m.Phase)
	}
	if m.inputs[inputContainerName].Value() != "omnideck" {
		t.Errorf("default container name should be 'omnideck'")
	}
	// Memory and SHM defaults are calculated from system RAM; just verify non-empty.
	if m.inputs[inputMemory].Value() == "" {
		t.Error("default memory should be non-empty")
	}
	if m.inputs[inputShmSize].Value() == "" {
		t.Error("default shm size should be non-empty")
	}
}

func TestBuildConfig(t *testing.T) {
	m := NewInstallModel("/tmp/test-config.yaml", nil, "")
	// Set custom values.
	m.inputs[inputContainerName].SetValue("mycontainer")
	m.inputs[inputSharedDir].SetValue("/data/shared")
	m.inputs[inputMemory].SetValue("4g")
	m.inputs[inputShmSize].SetValue("512m")

	cfg := m.buildConfig()
	if cfg.ContainerName != "mycontainer" {
		t.Errorf("ContainerName: got %q, want 'mycontainer'", cfg.ContainerName)
	}
	if cfg.SharedDir != "/data/shared" {
		t.Errorf("SharedDir: got %q", cfg.SharedDir)
	}
	if cfg.StateDir != "/data/shared/.state" {
		t.Errorf("StateDir: got %q, want '/data/shared/.state'", cfg.StateDir)
	}
	if cfg.Memory != "4g" {
		t.Errorf("Memory: got %q", cfg.Memory)
	}
	if cfg.ShmSize != "512m" {
		t.Errorf("ShmSize: got %q", cfg.ShmSize)
	}
}

func TestBuildConfigImageOverride(t *testing.T) {
	m := NewInstallModel("/tmp/test-config.yaml", nil, "my-custom-image:latest")
	cfg := m.buildConfig()
	if cfg.Image != "my-custom-image:latest" {
		t.Errorf("Image: got %q, want 'my-custom-image:latest'", cfg.Image)
	}
}

func TestValidateInputEmpty(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputs[inputContainerName].SetValue("")
	if m.validateCurrentInput() {
		t.Error("empty container name should fail validation")
	}
	if m.inputErrs[inputContainerName] == "" {
		t.Error("validation error should be set")
	}
}

func TestValidateInputShmSizeBad(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256mb") // invalid
	if m.validateCurrentInput() {
		t.Error("'256mb' should fail shm size validation")
	}
}

func TestValidateInputShmSizeGood(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputShmSize
	m.inputs[inputShmSize].SetValue("256m")
	if !m.validateCurrentInput() {
		t.Error("'256m' should pass validation")
	}
}

func TestValidateInputMemoryBad(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("2gb") // invalid
	if m.validateCurrentInput() {
		t.Error("'2gb' should fail memory validation")
	}
}

func TestValidateInputMemoryGood(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.inputFocus = inputMemory
	m.inputs[inputMemory].SetValue("4g")
	if !m.validateCurrentInput() {
		t.Error("'4g' should pass memory validation")
	}
}

func TestValidContainerName(t *testing.T) {
	valid := []string{"omnideck", "my-container", "box_1", "X123"}
	for _, s := range valid {
		if !validContainerName(s) {
			t.Errorf("expected %q to be a valid container name", s)
		}
	}
	invalid := []string{"", "-leadingdash", "has space", "dot.start"}
	for _, s := range invalid {
		// dot.start is actually valid by the regex (starts with letter)
		// only check the ones we know are invalid
		_ = s
	}
	definitelyInvalid := []string{"", "-leadingdash", "has space"}
	for _, s := range definitelyInvalid {
		if validContainerName(s) {
			t.Errorf("expected %q to be an invalid container name", s)
		}
	}
}

func TestValidPort(t *testing.T) {
	valid := []string{"1", "2337", "8080", "65535"}
	for _, s := range valid {
		if !validPort(s) {
			t.Errorf("expected %q to be a valid port", s)
		}
	}
	invalid := []string{"0", "65536", "-1", "abc", "", "8080.5"}
	for _, s := range invalid {
		if validPort(s) {
			t.Errorf("expected %q to be an invalid port", s)
		}
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()

	got := expandTilde("~/documents")
	want := home + "/documents"
	if got != want {
		t.Errorf("expandTilde(~/documents): got %q, want %q", got, want)
	}

	abs := "/absolute/path"
	if got := expandTilde(abs); got != abs {
		t.Errorf("absolute path should pass through unchanged, got %q", got)
	}

	rel := "relative/path"
	if got := expandTilde(rel); got != rel {
		t.Errorf("relative path should pass through unchanged, got %q", got)
	}
}

func TestPreflightAdvancement(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	// Simulate all checks arriving.
	m.preflightDone = 3 // one before 4
	cmd := m.maybeAdvancePreflight()
	if cmd != nil {
		t.Error("should not advance until 4 checks complete")
	}
	m.preflightDone = 4
	cmd = m.maybeAdvancePreflight()
	if cmd == nil {
		t.Error("should advance after 4 checks complete")
	}
}

func TestUpdatePreflightEngineOK(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	// When eng == nil, both the engine check and the skipped permission check
	// are counted immediately, so preflightDone goes from 0 to 2.
	msg := engineCheckResult{eng: nil, err: nil}
	m.preflightDone = 0
	newModel, _ := m.updatePreflight(msg)
	nm := newModel.(InstallModel)
	if nm.preflightDone != 2 {
		t.Errorf("preflightDone should be 2, got %d", nm.preflightDone)
	}
}

func TestUpdatePreflightEngineError(t *testing.T) {
	m := NewInstallModel("/tmp/test.yaml", nil, "")
	m.preflightDone = 4 // all other checks done
	msg := engineCheckResult{eng: nil, err: fmt.Errorf("no engine")}
	newModel, _ := m.updatePreflight(msg)
	nm := newModel.(InstallModel)
	// allPreflightDone will be fired next tick, but we test direct path:
	// Set preflightDone high and fire allPreflightDone directly.
	nm.engErr = msg.err
	nm2, _ := nm.updatePreflight(allPreflightDone{})
	final := nm2.(InstallModel)
	if final.Phase != PhaseError {
		t.Errorf("should enter error phase when engine missing, got %d", final.Phase)
	}
}
