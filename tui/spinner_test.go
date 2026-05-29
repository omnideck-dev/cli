package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestNewSpinnerModel(t *testing.T) {
	labels := []string{"Step 1", "Step 2", "Step 3"}
	m := NewSpinnerModel(labels, DefaultFlavorMessages)

	if m.StepCount() != 3 {
		t.Errorf("expected 3 steps, got %d", m.StepCount())
	}
	for i, s := range m.Steps {
		if s.Status != StepPending {
			t.Errorf("step %d: expected Pending, got %d", i, s.Status)
		}
		if s.Label != labels[i] {
			t.Errorf("step %d: label mismatch", i)
		}
	}
}

func TestSpinnerStepDone(t *testing.T) {
	m := NewSpinnerModel([]string{"A", "B"}, nil)
	m, _ = m.Update(StepStartedMsg{Index: 0})
	m, _ = m.Update(StepDoneMsg{Index: 0, Detail: "ok"})

	if m.Steps[0].Status != StepDone {
		t.Errorf("step 0 should be Done")
	}
	if m.Steps[0].Detail != "ok" {
		t.Errorf("detail mismatch: %q", m.Steps[0].Detail)
	}
	if m.CurrentStep != 1 {
		t.Errorf("CurrentStep should advance to 1, got %d", m.CurrentStep)
	}
}

func TestSpinnerStepFailed(t *testing.T) {
	m := NewSpinnerModel([]string{"A", "B"}, nil)
	m, _ = m.Update(StepStartedMsg{Index: 0})
	testErr := errors.New("boom")
	m, _ = m.Update(StepFailedMsg{Index: 0, Err: testErr})

	if m.Steps[0].Status != StepFailed {
		t.Errorf("step 0 should be Failed")
	}
	if m.Error() != testErr {
		t.Errorf("Error() should return the step error")
	}
}

func TestSpinnerAllDone(t *testing.T) {
	m := NewSpinnerModel([]string{"A"}, nil)
	m, _ = m.Update(StepStartedMsg{Index: 0})
	m, _ = m.Update(StepDoneMsg{Index: 0})

	if !m.IsDone() {
		t.Error("IsDone should be true after all steps complete")
	}
}

func TestSpinnerViewShowsCompleted(t *testing.T) {
	m := NewSpinnerModel([]string{"Step 1", "Step 2"}, nil)
	m, _ = m.Update(StepStartedMsg{Index: 0})
	m, _ = m.Update(StepDoneMsg{Index: 0, Detail: "done!"})

	view := m.View()
	if !strings.Contains(view, "Step 1") {
		t.Errorf("view should show completed step label: %q", view)
	}
}

func TestSpinnerViewShowsError(t *testing.T) {
	m := NewSpinnerModel([]string{"Step 1"}, nil)
	m, _ = m.Update(StepStartedMsg{Index: 0})
	m, _ = m.Update(StepFailedMsg{Index: 0, Err: errors.New("oops")})

	view := m.View()
	if !strings.Contains(view, "oops") {
		t.Errorf("view should show error text: %q", view)
	}
}

func TestFlavorRotation(t *testing.T) {
	flavors := []string{"msg1", "msg2", "msg3"}
	m := NewSpinnerModel([]string{"A"}, flavors)

	if m.ActiveFlavor() != "msg1" {
		t.Errorf("initial flavor should be msg1, got %q", m.ActiveFlavor())
	}
	m, _ = m.Update(flavorTickMsg{})
	if m.ActiveFlavor() != "msg2" {
		t.Errorf("after tick, flavor should be msg2, got %q", m.ActiveFlavor())
	}
	m, _ = m.Update(flavorTickMsg{})
	m, _ = m.Update(flavorTickMsg{})
	// wraps around: 0→1→2→0
	if m.ActiveFlavor() != "msg1" {
		t.Errorf("flavor should wrap around: got %q", m.ActiveFlavor())
	}
}

func TestStepCmd(t *testing.T) {
	cmd := StepCmd(2, func() (string, error) {
		return "great", nil
	})
	msg := cmd()
	done, ok := msg.(StepDoneMsg)
	if !ok {
		t.Fatalf("expected StepDoneMsg, got %T", msg)
	}
	if done.Index != 2 {
		t.Errorf("index: got %d, want 2", done.Index)
	}
	if done.Detail != "great" {
		t.Errorf("detail: got %q, want 'great'", done.Detail)
	}
}

func TestStepCmdError(t *testing.T) {
	cmd := StepCmd(1, func() (string, error) {
		return "", errors.New("fail")
	})
	msg := cmd()
	failed, ok := msg.(StepFailedMsg)
	if !ok {
		t.Fatalf("expected StepFailedMsg, got %T", msg)
	}
	if failed.Index != 1 {
		t.Errorf("index: got %d, want 1", failed.Index)
	}
}
