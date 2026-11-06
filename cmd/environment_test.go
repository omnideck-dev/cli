package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
	"github.com/omnideck-dev/cli/workflow"
)

func TestDesiredEnvironmentUpdatesLayoutWithoutMutatingSavedConfig(t *testing.T) {
	current := config.DefaultConfig()
	current.LayoutVersion = 1
	desired, err := desiredEnvironmentConfig(current)
	if err != nil {
		t.Fatal(err)
	}
	if current.LayoutVersion != 1 || desired.LayoutVersion != config.CurrentContainerLayout || desired.LayoutVersion <= current.LayoutVersion {
		t.Fatalf("current=%d desired=%d", current.LayoutVersion, desired.LayoutVersion)
	}
}

func TestDesiredEnvironmentConfigKeepsAnExistingInstancesContainer(t *testing.T) {
	originalName := nameFlag
	t.Cleanup(func() { nameFlag = originalName })
	nameFlag = "desktop"

	current := config.DefaultConfig()
	current.ContainerName = "omnideck-desktop"
	desired, err := desiredEnvironmentConfig(current)
	if err != nil {
		t.Fatal(err)
	}
	if desired.ContainerName != "omnideck-desktop" {
		t.Fatalf("existing instance container = %q, want omnideck-desktop", desired.ContainerName)
	}

	created, err := desiredEnvironmentConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.ContainerName != "desktop" {
		t.Fatalf("new instance container = %q, want desktop", created.ContainerName)
	}
}

func TestDesiredEnvironmentConfigRejectsSharedMemoryAboveMemoryLimit(t *testing.T) {
	originalMemory, originalSHM := environmentMemoryFlag, environmentShmFlag
	t.Cleanup(func() {
		environmentMemoryFlag = originalMemory
		environmentShmFlag = originalSHM
	})
	environmentMemoryFlag = "1g"
	environmentShmFlag = "2g"

	_, err := desiredEnvironmentConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "--shm-size cannot be larger than --memory") {
		t.Fatalf("desiredEnvironmentConfig() error = %v", err)
	}
}

func TestEnvironmentJSONErrorUsesWorkflowClassifications(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "port", err: errors.Join(workflow.ErrPortInUse, errors.New("localized detail")), want: ErrCodePortInUse},
		{name: "saved installation port wording", err: errors.New("another Omnideck installation already uses port 2337"), want: ErrCodePortInUse},
		{name: "container", err: errors.Join(workflow.ErrContainerConflict, errors.New("localized detail")), want: ErrCodeContainerConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := environmentJSONError(tt.err).payload.Code; got != tt.want {
				t.Fatalf("code = %q, want %q", got, tt.want)
			}
		})
	}

	if got := environmentStageJSONError("unknown", errors.Join(workflow.ErrImageDownload, errors.New("localized detail"))).payload.Code; got != ErrCodeDownloadFailed {
		t.Fatalf("download code = %q, want %q", got, ErrCodeDownloadFailed)
	}
}

func TestDesiredEnvironmentConfigAllowsSharedMemoryAtMemoryLimit(t *testing.T) {
	originalMemory, originalSHM := environmentMemoryFlag, environmentShmFlag
	t.Cleanup(func() {
		environmentMemoryFlag = originalMemory
		environmentShmFlag = originalSHM
	})
	environmentMemoryFlag = "1g"
	environmentShmFlag = "1024m"

	if _, err := desiredEnvironmentConfig(nil); err != nil {
		t.Fatalf("desiredEnvironmentConfig() error = %v", err)
	}
}

func TestValidateEnvironmentOwnershipRefusesAnotherInstallationsContainerOrData(t *testing.T) {
	t.Setenv("OMNIDECK_CONFIG_DIR", t.TempDir())
	other := &config.Config{ContainerName: "omnideck-desktop", Memory: "2g", ShmSize: "1g", WebUIPort: "2337", Image: config.DefaultImage}
	if err := config.Save(config.InstancePath("desktop"), other); err != nil {
		t.Fatal(err)
	}
	originalName := nameFlag
	t.Cleanup(func() { nameFlag = originalName })

	for _, tc := range []struct {
		name      string
		instance  string
		container string
		home      string
		wantTaken bool
	}{
		{"new instance named after its container", "omnideck-desktop", "omnideck-desktop", "", true},
		{"new instance reusing its home volume", "work", "work", "omnideck-desktop-home", true},
		{"the installation itself", "desktop", "omnideck-desktop", "", false},
		{"unrelated new instance", "work", "work", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nameFlag = tc.instance
			desired := &config.Config{ContainerName: tc.container, HomeVolume: tc.home, Memory: "2g", ShmSize: "1g", WebUIPort: "2400", Image: config.DefaultImage}
			err := validateEnvironmentOwnership(desired)
			if taken := errors.Is(err, workflow.ErrInstanceNameTaken); taken != tc.wantTaken {
				t.Fatalf("err = %v, want ErrInstanceNameTaken %v", err, tc.wantTaken)
			}
			if !tc.wantTaken && err != nil {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// The hint decides whether a GUI offers to delete a container, so it must
// follow the error's type, never its wording.
func TestEnvironmentJSONErrorNeverSuggestsRemovingAnotherInstallation(t *testing.T) {
	taken := environmentJSONError(workflow.InstanceNameTakenError("omnideck-desktop")).payload
	if taken.Code != ErrCodeContainerConflict || !strings.Contains(taken.Hint, "do not remove it") || strings.Contains(taken.Hint, "remove the unrelated container") {
		t.Fatalf("instance name taken = %+v", taken)
	}
	unrelated := environmentJSONError(workflow.ContainerConflictError("work")).payload
	if unrelated.Code != ErrCodeContainerConflict || !strings.Contains(unrelated.Hint, "remove the unrelated container") {
		t.Fatalf("unrelated container = %+v", unrelated)
	}
	if code := environmentJSONError(errors.New("another container already uses the name \"work\"")).payload.Code; code != ErrCodeInternal {
		t.Fatalf("untyped wording classified as %q; only the error type may choose the conflict hint", code)
	}
}
