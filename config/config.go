package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all persisted settings for omnideck.
type Config struct {
	ContainerName string `yaml:"container_name"`
	HomeVolume    string `yaml:"home_volume,omitempty"`
	StateVolume   string `yaml:"state_volume,omitempty"`
	Memory        string `yaml:"memory"` // container --memory limit (e.g. "2g")
	ShmSize       string `yaml:"shm_size"`
	WebUIPort     string `yaml:"web_ui_port"` // host port for the web UI (default "2337")
	// Engine is retained only to read configurations created before runtime
	// selection became shared. New configurations leave it empty.
	Engine      string    `yaml:"engine,omitempty"`
	Image       string    `yaml:"image"`
	InstalledAt time.Time `yaml:"installed_at"`
}

// DefaultImage is the current container image the CLI installs and updates to.
const DefaultImage = "ghcr.io/omnideck-dev/omnideck:main"

// legacyImagePrefixes are image repositories that DefaultImage supersedes.
// A config still pointing at one of these (regardless of tag) is migrated to
// DefaultImage on update. Intentional custom images (e.g. via --image) are
// left untouched.
var legacyImagePrefixes = []string{
	"ghcr.io/lefoulkrod/computron_9000",
}

// InstanceInfo is a resolved instance with its name, path, and loaded Config.
type InstanceInfo struct {
	Name   string
	Path   string
	Config *Config
}

// instancesDirOverride is set in tests to avoid touching the real config dir.
var instancesDirOverride string

// Dir returns the directory containing Omnideck's machine-wide settings and
// instance configurations. OMNIDECK_CONFIG_DIR is intended for isolated test
// and automation runs.
func Dir() string {
	if override := os.Getenv("OMNIDECK_CONFIG_DIR"); override != "" {
		return expandHome(override)
	}
	return expandHome("~/.config/omnideck-cli")
}

// InstancesDir returns the directory where per-instance config files live.
func InstancesDir() string {
	if instancesDirOverride != "" {
		return instancesDirOverride
	}
	return filepath.Join(Dir(), "instances")
}

// InstancePath returns the config path for a named instance.
func InstancePath(name string) string {
	return filepath.Join(InstancesDir(), name+".yaml")
}

// ListInstances scans InstancesDir for *.yaml files and returns all valid instances.
func ListInstances() ([]InstanceInfo, error) {
	dir := InstancesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var instances []InstanceInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		cfg, err := Load(path)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		instances = append(instances, InstanceInfo{Name: name, Path: path, Config: cfg})
	}
	return instances, nil
}

// DefaultPath returns the legacy single-file config path (kept for --config flag use).
func DefaultPath() string {
	return filepath.Join(Dir(), "config.yaml")
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ContainerName: "omnideck",
		Memory:        "2g",
		ShmSize:       "1024m",
		WebUIPort:     "2337",
		Image:         DefaultImage,
	}
}

// HomeVolumeName returns the configured home volume or derives one from the
// container name. Empty keeps older configs usable without migration.
func (c *Config) HomeVolumeName() string {
	if c.HomeVolume != "" {
		return c.HomeVolume
	}
	return c.ContainerName + "-home"
}

// StateVolumeName returns the configured state volume or derives one from the
// container name. Empty keeps older configs usable without migration.
func (c *Config) StateVolumeName() string {
	if c.StateVolume != "" {
		return c.StateVolume
	}
	return c.ContainerName + "-state"
}

// MigrateImage updates a legacy image reference to DefaultImage. It returns
// true if the image was changed, so callers can persist the migrated config.
// Images that are not recognized as legacy (including custom overrides) are
// left as-is.
func (c *Config) MigrateImage() bool {
	for _, prefix := range legacyImagePrefixes {
		if c.Image == prefix || strings.HasPrefix(c.Image, prefix+":") {
			c.Image = DefaultImage
			return true
		}
	}
	return false
}

// WebUIPortOrDefault returns WebUIPort, falling back to "8080" for configs
// written before the port field was added.
func (c *Config) WebUIPortOrDefault() string {
	if c.WebUIPort == "" {
		return "2337"
	}
	return c.WebUIPort
}

// Load reads a YAML config file from path. Expands ~ in path.
func Load(path string) (*Config, error) {
	path = expandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes a Config as YAML to path, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
