package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/workflow"
)

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
