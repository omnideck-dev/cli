package workflow

import (
	"os"
	"testing"
)

// TestMain keeps every test away from the real saved instances: workflows
// such as RemoveInstance read the configuration directory themselves.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "omnideck-workflow-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("OMNIDECK_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
