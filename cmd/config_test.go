package cmd

import "testing"

func TestIsValidConfigKey(t *testing.T) {
	valid := []string{"home_volume", "state_volume", "memory", "shm_size", "web_ui_port", "image"}
	invalid := []string{"container_name", "engine", "installed_at", "foo", ""}

	for _, k := range valid {
		if !isValidConfigKey(k) {
			t.Errorf("expected %q to be a valid config key", k)
		}
	}
	for _, k := range invalid {
		if isValidConfigKey(k) {
			t.Errorf("expected %q to be an invalid config key", k)
		}
	}
}
