package workflow

import (
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestNewInstanceDefaultsSkipsBusyPortsAndExistingFiles(t *testing.T) {
	instances := []config.InstanceInfo{
		{Name: "omnideck", Config: &config.Config{ContainerName: "omnideck", WebUIPort: "2337"}},
	}
	cfg := NewInstanceDefaultsWith(instances, InstanceDefaultsOptions{
		PortAvailable:  func(port string) bool { return port != "2338" && port != "2339" },
		InstanceExists: func(name string) bool { return name == "omnideck2" },
	})
	if cfg.ContainerName != "omnideck3" || cfg.WebUIPort != "2340" {
		t.Fatalf("defaults = %s:%s, want omnideck3:2340", cfg.ContainerName, cfg.WebUIPort)
	}
}
