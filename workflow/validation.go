package workflow

import (
	"fmt"
	"strings"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
)

// ConfigValidationError identifies the persisted field that failed shared
// instance validation. Front ends may translate Field into their own flag or
// form label without duplicating the validation policy.
type ConfigValidationError struct {
	Field   string
	Message string
}

func (e *ConfigValidationError) Error() string { return e.Message }

// ValidateInstanceConfig applies the cross-surface configuration policy used
// by setup, environment reconciliation, and the underlying lifecycle.
func ValidateInstanceConfig(cfg *config.Config) error {
	if cfg == nil {
		return &ConfigValidationError{Field: "config", Message: "instance configuration is required"}
	}
	if !checks.ValidContainerName(cfg.ContainerName) {
		return &ConfigValidationError{Field: "name", Message: "name must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens"}
	}
	if !checks.ValidPort(cfg.WebUIPortOrDefault()) {
		return &ConfigValidationError{Field: "port", Message: "port must be a number between 1 and 65535"}
	}
	if !checks.ValidMemorySize(cfg.Memory) {
		return &ConfigValidationError{Field: "memory", Message: "memory must be a positive number and unit, such as 2g"}
	}
	if !checks.ValidMemorySize(cfg.ShmSize) {
		return &ConfigValidationError{Field: "shm_size", Message: "shared memory must be a positive number and unit, such as 512m"}
	}
	memoryMB, _ := checks.MemorySizeMB(cfg.Memory)
	shmMB, _ := checks.MemorySizeMB(cfg.ShmSize)
	if shmMB > memoryMB {
		return &ConfigValidationError{Field: "shm_size", Message: "shared memory cannot be larger than memory"}
	}
	if strings.TrimSpace(cfg.Image) == "" {
		return &ConfigValidationError{Field: "image", Message: "image cannot be empty"}
	}
	for field, volume := range map[string]string{
		"home_volume":  cfg.HomeVolume,
		"state_volume": cfg.StateVolume,
	} {
		if volume != "" && !checks.ValidContainerName(volume) {
			return &ConfigValidationError{Field: field, Message: fmt.Sprintf("%s must use only letters, numbers, dots, underscores, or hyphens", strings.ReplaceAll(field, "_", " "))}
		}
	}
	return nil
}
