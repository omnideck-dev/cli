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
	ContainerName string    `yaml:"container_name"`
	SharedDir     string    `yaml:"shared_dir"`
	StateDir      string    `yaml:"state_dir"`
	Memory        string    `yaml:"memory"`      // container --memory limit (e.g. "2g")
	ShmSize       string    `yaml:"shm_size"`
	WebUIPort     string    `yaml:"web_ui_port"` // host port for the web UI (default "2337")
	Engine        string    `yaml:"engine"`      // "docker" or "podman"
	Image         string    `yaml:"image"`
	InstalledAt   time.Time `yaml:"installed_at"`
}

// InstanceInfo is a resolved instance with its name, path, and loaded Config.
type InstanceInfo struct {
	Name   string
	Path   string
	Config *Config
}

// instancesDirOverride is set in tests to avoid touching the real config dir.
var instancesDirOverride string

// InstancesDir returns the directory where per-instance config files live.
func InstancesDir() string {
	if instancesDirOverride != "" {
		return instancesDirOverride
	}
	return expandHome("~/.config/omnideck-cli/instances")
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
	return expandHome("~/.config/omnideck-cli/config.yaml")
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ContainerName: "omnideck",
		SharedDir:     expandHome("~/Omnideck"),
		StateDir:      expandHome("~/Omnideck/.state"),
		Memory:        "2g",
		ShmSize:       "1024m",
		WebUIPort:     "2337",
		Image:         "ghcr.io/lefoulkrod/computron_9000:main",
	}
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
	cfg.SharedDir = expandHome(cfg.SharedDir)
	cfg.StateDir = expandHome(cfg.StateDir)
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
