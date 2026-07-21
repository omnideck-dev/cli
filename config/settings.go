package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings contains choices shared by every Omnideck instance for this user.
// Instance-specific values continue to live in instances/*.yaml.
type Settings struct {
	Runtime string `yaml:"runtime,omitempty"`
}

var settingsPathOverride string

// SettingsPath returns the path to shared Omnideck settings.
func SettingsPath() string {
	if settingsPathOverride != "" {
		return settingsPathOverride
	}
	return filepath.Join(Dir(), "settings.yaml")
}

// LoadSettings reads shared settings. A missing file means that setup
// has not selected a runtime yet and is not an error.
func LoadSettings() (*Settings, error) {
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil
		}
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing settings: %w", err)
	}
	if settings.Runtime != "" && !ValidRuntime(settings.Runtime) {
		return nil, fmt.Errorf("settings contain unknown runtime %q", settings.Runtime)
	}
	return &settings, nil
}

// SaveRuntime records the one container runtime used by all of this user's
// Omnideck instances.
func SaveRuntime(name string) error {
	if !ValidRuntime(name) {
		return fmt.Errorf("unknown container runtime %q", name)
	}
	path := SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}
	data, err := yaml.Marshal(&Settings{Runtime: name})
	if err != nil {
		return fmt.Errorf("preparing settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}

// ValidRuntime reports whether name is a runtime Omnideck supports.
func ValidRuntime(name string) bool {
	return name == "docker" || name == "podman"
}
