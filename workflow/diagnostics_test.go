package workflow

import (
	"testing"

	"github.com/omnideck-dev/cli/engine"
)

func TestDoctorRuntimeCheckIgnoresUnsupportedDockerProbe(t *testing.T) {
	result, usable := doctorRuntimeCheck(nil, []engine.ProbeResult{{
		Name:    "docker",
		State:   engine.RuntimeReady,
		Version: "27.0.0",
	}})
	if usable != nil {
		t.Fatalf("usable engine = %v, want nil", usable)
	}
	if result.Status != CheckFail || result.Action != DoctorActionRuntimeSetup || result.ActionValue != "podman" {
		t.Fatalf("runtime result = %#v", result)
	}
}
