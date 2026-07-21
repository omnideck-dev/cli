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
			return fmt.Errorf("home_volume must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens")
		}
		cfg.HomeVolume = value
	case "state_volume":
		if value != "" && !checks.ValidContainerName(value) {
			return fmt.Errorf("state_volume must start with a letter or number and use only letters, numbers, dots, underscores, or hyphens")
		}
		cfg.StateVolume = value
	case "memory":
		if !checks.ValidMemorySize(value) {
			return fmt.Errorf("memory must be a number and unit, such as 2g")
		}
		cfg.Memory = value
	case "shm_size":
		if !checks.ValidMemorySize(value) {
			return fmt.Errorf("shm_size must be a number and unit, such as 512m or 2g")
		}
		cfg.ShmSize = value
	case "web_ui_port":
		if !checks.ValidPort(value) {
			return fmt.Errorf("web_ui_port must be a number between 1 and 65535")
		}
		cfg.WebUIPort = value
	case "image":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("image cannot be empty")
		}
		cfg.Image = value
	default:
		return fmt.Errorf("setting %q cannot be changed", key)
	}
	return nil
}
