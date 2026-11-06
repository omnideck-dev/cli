package workflow

import (
	"fmt"
	"strconv"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
)

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
		if instance.Config == nil {
			continue
		}
		takenNames[instance.Name] = true
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
	if port, ok := checks.NextAvailablePortFunc(maxPort+1, takenPorts, portAvailable); ok {
		cfg.WebUIPort = port
	}
	return cfg
}
