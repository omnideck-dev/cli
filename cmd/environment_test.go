package cmd

import (
	"strings"
	"testing"
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
