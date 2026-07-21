package workflow

import (
	"fmt"
	"strconv"

	"github.com/omnideck-dev/cli/checks"
	"github.com/omnideck-dev/cli/config"
)

// NewInstanceDefaults returns setup defaults that do not conflict with known
// Omnideck instance names or browser ports.
func NewInstanceDefaults(instances []config.InstanceInfo) *config.Config {
	takenNames := map[string]bool{}
	takenPorts := map[string]bool{}
	maxPort := 2336
	for _, instance := range instances {
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
		if !takenNames[name] {
			cfg.ContainerName = name
			break
		}
	}
	if port, ok := checks.NextAvailablePort(maxPort+1, takenPorts); ok {
		cfg.WebUIPort = port
	}
	return cfg
}
