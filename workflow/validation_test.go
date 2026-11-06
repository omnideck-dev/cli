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

func TestValidateInstanceChangeAppliesTheMemoryRuleOnlyToChanges(t *testing.T) {
	legacy := &config.Config{ContainerName: "demo", Memory: "1g", ShmSize: "2g", Image: config.DefaultImage}
	for _, tc := range []struct {
		name    string
		current *config.Config
		desired config.Config
		wantErr bool
	}{
		{"new instance", nil, *legacy, true},
		{"unchanged saved pair", legacy, *legacy, false},
		{"other setting changed", legacy, config.Config{ContainerName: "demo", Memory: "1g", ShmSize: "2g", Image: "example:v2"}, false},
		{"memory changed", legacy, config.Config{ContainerName: "demo", Memory: "1500m", ShmSize: "2g", Image: config.DefaultImage}, true},
		{"shm changed", legacy, config.Config{ContainerName: "demo", Memory: "1g", ShmSize: "3g", Image: config.DefaultImage}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateInstanceChange(tc.current, &tc.desired)
			var validation *ConfigValidationError
			if tc.wantErr != (err != nil) || (err != nil && (!errors.As(err, &validation) || validation.Field != "shm_size")) {
				t.Fatalf("ValidateInstanceChange() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
