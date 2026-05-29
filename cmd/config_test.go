package cmd

import "testing"

func TestIsValidConfigKey(t *testing.T) {
	valid := []string{"container_name", "shared_dir", "state_dir", "shm_size"}
	invalid := []string{"engine", "image", "installed_at", "foo", ""}

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
