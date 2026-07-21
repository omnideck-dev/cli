package workflow

import (
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestApplySettingSupportsTheSharedEditableSurface(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"home_volume", "custom-home"},
		{"state_volume", "custom-state"},
		{"memory", "4g"},
		{"shm_size", "512m"},
		{"web_ui_port", "2448"},
		{"image", "example.com/omnideck:test"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			cfg := config.DefaultConfig()
			if err := ApplySetting(cfg, test.key, test.value); err != nil {
				t.Fatalf("ApplySetting(%q): %v", test.key, err)
			}
		})
	}
}

func TestApplySettingRejectsInvalidValuesAndProtectedFields(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{"container_name", "renamed"},
		{"memory", "a lot"},
		{"web_ui_port", "70000"},
		{"image", ""},
	} {
		cfg := config.DefaultConfig()
		if err := ApplySetting(cfg, test.key, test.value); err == nil {
			t.Fatalf("ApplySetting(%q, %q) unexpectedly succeeded", test.key, test.value)
		}
	}
}
