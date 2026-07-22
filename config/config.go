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
// The latest tag is intentionally a moving channel: setup and update should
// always fetch the newest published Omnideck application image.
const DefaultImage = "ghcr.io/omnideck-dev/omnideck:latest"

// legacyImagePrefixes are image repositories that DefaultImage supersedes.
// A config still pointing at one of these (regardless of tag) is migrated to
// DefaultImage on update. Intentional custom images (e.g. via --image) are
// left untouched.
var legacyImagePrefixes = []string{
	"ghcr.io/lefoulkrod/computron_9000",
}

// legacyDefaultImages are exact historical defaults. Keep these separate from
// legacyImagePrefixes so an explicit Omnideck development tag remains a custom
// override instead of being silently replaced.
var legacyDefaultImages = []string{
	"ghcr.io/omnideck-dev/omnideck:main",
}

// InstanceInfo is a resolved instance with its name, path, and loaded Config.
type InstanceInfo struct {
	Name   string
	Path   string
	Config *Config
}

// instancesDirOverride is set in tests to avoid touching the real config dir.
var instancesDirOverride string

var userConfigDir = os.UserConfigDir
var userHomeDir = os.UserHomeDir

// Dir returns the platform-native directory containing Omnideck's shared
// settings and instance configurations. OMNIDECK_CONFIG_DIR is intended for
// isolated test and automation runs.
func Dir() string {
	if override := os.Getenv("OMNIDECK_CONFIG_DIR"); override != "" {
		return expandHome(override)
	}
	if base, err := userConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "omnideck-cli")
	}
	return legacyDir()
}

// MigrateLegacyDir copies configuration from the original cross-platform
// ~/.config location when the OS now provides a different conventional
// location. Existing files in the new location always win, and the originals
// are left in place so a migration can never remove a user's configuration.
func MigrateLegacyDir() error {
	if os.Getenv("OMNIDECK_CONFIG_DIR") != "" {
		return nil
	}

	source := legacyDir()
	target := Dir()
	if filepath.Clean(source) == filepath.Clean(target) {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking config directory: %w", err)
	}

	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking previous config directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("previous config path %s is not a directory", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("creating config parent directory: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(target), ".omnideck-cli-migration-*")
	if err != nil {
		return fmt.Errorf("preparing config migration: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, name := range []string{"settings.yaml", "config.yaml"} {
		if err := copyLegacyFile(filepath.Join(source, name), filepath.Join(staging, name)); err != nil {
			return err
		}
	}

	sourceInstances := filepath.Join(source, "instances")
	entries, err := os.ReadDir(sourceInstances)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading previous instance configs: %w", err)
		}
		entries = nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		if err := copyLegacyFile(
			filepath.Join(sourceInstances, entry.Name()),
			filepath.Join(staging, "instances", entry.Name()),
		); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		// Another Omnideck process may have completed migration first. Its
		// conventional directory remains authoritative in that case.
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return fmt.Errorf("finishing config migration: %w", err)
	}
	return nil
}

func legacyDir() string {
	home, err := userHomeDir()
	if err != nil || home == "" {
		return filepath.Join("~", ".config", "omnideck-cli")
	}
	return filepath.Join(home, ".config", "omnideck-cli")
}

func copyLegacyFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking previous config file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("reading previous config file: %w", err)
	}
	if err := ensurePrivateDir(filepath.Dir(target)); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("creating migrated config file: %w", err)
	}
	written := false
	defer func() {
		_ = f.Close()
		if !written {
			_ = os.Remove(target)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing migrated config file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing migrated config file: %w", err)
	}
	written = true
	return nil
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
	for _, image := range legacyDefaultImages {
		if c.Image == image {
			c.Image = DefaultImage
			return true
		}
	}
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
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return writePrivateFile(path, data)
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700) // #nosec G302 -- path is a directory, not a file
}

func writePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// WriteFile preserves the mode of an existing file. Tighten files created
	// by older CLI versions as soon as they are saved again.
	return os.Chmod(path, 0o600)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := userHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}
