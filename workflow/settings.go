package workflow

import (
	"fmt"
	"strings"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
)

// EditableSettingKeys is the supported configuration surface shared by the
// command-line editor and dashboard editor.
var EditableSettingKeys = []string{
	"home_volume",
	"state_volume",
	"memory",
	"shm_size",
	"web_ui_port",
	"image",
}

// ApplySetting validates and applies one editable setting to cfg.
func ApplySetting(cfg *config.Config, key, value string) error {
	switch key {
	case "home_volume":
		if value != "" && !checks.ValidContainerName(value) {
			return &ConfigValidationError{Field: "home_volume", Message: "home_volume must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens"}
		}
		cfg.HomeVolume = value
	case "state_volume":
		if value != "" && !checks.ValidContainerName(value) {
			return &ConfigValidationError{Field: "state_volume", Message: "state_volume must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens"}
		}
		cfg.StateVolume = value
	case "memory":
		if !checks.ValidMemorySize(value) {
			return &ConfigValidationError{Field: "memory", Message: "memory must be a number and unit, such as 2g"}
		}
		cfg.Memory = value
	case "shm_size":
		if !checks.ValidMemorySize(value) {
			return &ConfigValidationError{Field: "shm_size", Message: "shm_size must be a number and unit, such as 512m or 2g"}
		}
		cfg.ShmSize = value
	case "web_ui_port":
		if !checks.ValidPort(value) {
			return &ConfigValidationError{Field: "port", Message: "web_ui_port must be a number between 1 and 65535"}
		}
		cfg.WebUIPort = value
	case "image":
		if strings.TrimSpace(value) == "" {
			return &ConfigValidationError{Field: "image", Message: "image cannot be empty"}
		}
		cfg.Image = value
	default:
		return &ConfigValidationError{Field: key, Message: fmt.Sprintf("setting %q cannot be changed", key)}
	}
	return nil
}

// CheckMemoryLimits applies ValidateInstanceConfig's rule between the two
// memory settings. Call it once after applying every changed setting, not per
// field, so lowering both together is judged on the final values. A missing
// or unreadable value is not judged here; full validation reports it when the
// container is next created, so older saved settings stay editable.
//
// changed is the setting the caller is changing ("memory" or "shm_size"), so
// the error names it. Any other value means neither changed: a pair saved by
// an older CLI is only judged once one of them is edited.
func CheckMemoryLimits(cfg *config.Config, changed string) error {
	if changed != "memory" && changed != "shm_size" {
		return nil
	}
	memoryMB, memoryOK := checks.MemorySizeMB(cfg.Memory)
	shmMB, shmOK := checks.MemorySizeMB(cfg.ShmSize)
	if !memoryOK || !shmOK || shmMB <= memoryMB {
		return nil
	}
	if changed == "memory" {
		return &ConfigValidationError{Field: "memory", Message: fmt.Sprintf("memory cannot be smaller than shared memory (shm_size %s); lower shm_size first", cfg.ShmSize)}
	}
	return &ConfigValidationError{Field: "shm_size", Message: fmt.Sprintf("shared memory cannot be larger than memory (%s)", cfg.Memory)}
}
