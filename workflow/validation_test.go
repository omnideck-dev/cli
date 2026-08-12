package workflow

import (
	"errors"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestValidateInstanceConfigAppliesCrossFieldMemoryPolicy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ContainerName = "main"
	cfg.Memory = "1g"
	cfg.ShmSize = "2g"
	err := ValidateInstanceConfig(cfg)
	var validation *ConfigValidationError
	if !errors.As(err, &validation) || validation.Field != "shm_size" {
		t.Fatalf("err = %#v, want shm_size validation", err)
	}
}

func TestValidateInstanceConfigAcceptsDerivedVolumeNames(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ContainerName = "main"
	if err := ValidateInstanceConfig(cfg); err != nil {
		t.Fatal(err)
	}
}
