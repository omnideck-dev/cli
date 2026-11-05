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

func TestDoctorExternalURLCheck(t *testing.T) {
	// The instance name differs from the container name, as with Desktop's
	// "desktop" instance; hints must use the name `--name` accepts.
	cfg := &config.Config{ContainerName: "omnideck-work", WebUIPort: "2339"}
	for _, tc := range []struct {
		name   string
		env    []string
		status CheckStatus
		detail string
	}{
		{"ok", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:2339"}, CheckPass, "MCP sign-in address is http://localhost:2339"},
		{"missing", []string{"PORT=8080"}, CheckWarn, "created without its sign-in address"},
		{"mismatch", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:9999"}, CheckWarn, "uses http://localhost:9999 instead of http://localhost:2339"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := doctorExternalURLCheck("work", cfg, &fakeContainerEngine{env: tc.env})
			if result.Status != tc.status || !strings.Contains(result.Detail, tc.detail) {
				t.Fatalf("result = %#v", result)
			}
			if tc.status == CheckWarn && result.Hint != "Run: omnideck update --name work (saved data is kept)" {
				t.Fatalf("hint = %q", result.Hint)
			}
			if id := diagnosticID(result.Label); id != "instance.external_url" {
				t.Fatalf("id = %q", id)
			}
		})
	}
}

func TestInstanceCommandOmitsNameWhenInstanceCannotBeSelected(t *testing.T) {
	if got := InstanceCommand("update", "work"); got != "omnideck update --name work" {
		t.Fatalf("named = %q", got)
	}
	if got := InstanceCommand("update", ""); got != "omnideck update" {
		t.Fatalf("unnamed = %q", got)
	}
}
