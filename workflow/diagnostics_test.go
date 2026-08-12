package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
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

func TestDiagnoseInventoryIssuesHasStableIDAndPreservesPath(t *testing.T) {
	results := DiagnoseInventoryIssues([]config.InstanceIssue{{
		Name: "broken main", Path: "/tmp/broken main.yaml", Err: errors.New("bad yaml"),
	}})
	if len(results) != 1 || results[0].ID != "config.instance_file.broken_main" || results[0].Status != CheckFail {
		t.Fatalf("results = %#v", results)
	}
	if !strings.Contains(results[0].Hint, "/tmp/broken main.yaml") {
		t.Fatalf("hint = %q", results[0].Hint)
	}
}
