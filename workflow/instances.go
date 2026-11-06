package workflow

import (
	"fmt"
	"strconv"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
)

// CheckInstanceOwnership returns ErrInstanceNameTaken when cfg's container or
// one of its data volumes belongs to a saved installation other than self
// (the instance being created or reconciled; "" when there is none). Two
// instance files sharing a container or volume would let removing either one
// destroy the other's data.
func CheckInstanceOwnership(instances []config.InstanceInfo, self string, cfg *config.Config) error {
	for _, instance := range instances {
		if instance.Name == self || instance.Config == nil {
			continue
		}
		other := instance.Config
		if other.ContainerName == cfg.ContainerName {
			return InstanceNameTakenError(cfg.ContainerName)
		}
		for _, volume := range []string{cfg.HomeVolumeName(), cfg.StateVolumeName()} {
			if volume == other.HomeVolumeName() || volume == other.StateVolumeName() {
				return classifyError(ErrInstanceNameTaken, fmt.Errorf("another Omnideck installation (%s) already uses the data volume %q; choose a different --name", instance.Name, volume))
			}
		}
	}
	return nil
}

// InstanceDefaultsOptions replaces the host probes used to choose defaults.
// Nil fields use the real network and saved configuration directory.
type InstanceDefaultsOptions struct {
	PortAvailable  func(port string) bool
	InstanceExists func(name string) bool
}

// NewInstanceDefaults returns setup defaults that do not conflict with known
// Omnideck instance names or browser ports.
func NewInstanceDefaults(instances []config.InstanceInfo) *config.Config {
	return NewInstanceDefaultsWith(instances, InstanceDefaultsOptions{})
}

// NewInstanceDefaultsWith is NewInstanceDefaults with injectable host probes.
func NewInstanceDefaultsWith(instances []config.InstanceInfo, opts InstanceDefaultsOptions) *config.Config {
	portAvailable := checks.PortAvailable
	if opts.PortAvailable != nil {
		portAvailable = opts.PortAvailable
	}
	instanceExists := config.InstanceFileExists
	if opts.InstanceExists != nil {
		instanceExists = opts.InstanceExists
	}

	takenNames := map[string]bool{}
	takenPorts := map[string]bool{}
	maxPort := 2336
	for _, instance := range instances {
		// An unreadable config still owns its name.
		takenNames[instance.Name] = true
		if instance.Config == nil {
			continue
		}
		takenNames[instance.Config.ContainerName] = true
		takenPorts[instance.Config.WebUIPortOrDefault()] = true
		if port, err := strconv.Atoi(instance.Config.WebUIPortOrDefault()); err == nil && port > maxPort {
			maxPort = port
		}
	}

	cfg := config.DefaultConfig()
	for suffix := 1; ; suffix++ {
		name := "omnideck"
		if suffix > 1 {
			name = fmt.Sprintf("omnideck%d", suffix)
		}
		if !takenNames[name] && !instanceExists(name) {
			cfg.ContainerName = name
			break
		}
	}
	if port, ok := checks.NextAvailablePort(maxPort+1, takenPorts, portAvailable); ok {
		cfg.WebUIPort = port
	}
	return cfg
}
