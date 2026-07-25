package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestInstanceRemoveIsTheOnlyRemovalCommand(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"instance", "remove"})
	if err != nil {
		t.Fatal(err)
	}
	if command != instanceRemoveCmd {
		t.Fatalf("instance remove resolves to %q", command.CommandPath())
	}
	command, remaining, err := rootCmd.Find([]string{"uninstall"})
	if err == nil && command != rootCmd && len(remaining) == 0 {
		t.Fatal("uninstall must not remain as a command or alias")
	}
}

func TestPromptYesNoHonorsSafeDefaults(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n\nyes\nno\n"))
	if promptYesNo(scanner, "", false) {
		t.Fatal("empty answer with default no = yes")
	}
	if !promptYesNo(scanner, "", true) {
		t.Fatal("empty answer with default yes = no")
	}
	if !promptYesNo(scanner, "", false) {
		t.Fatal("yes answer = no")
	}
	if promptYesNo(scanner, "", true) {
		t.Fatal("no answer = yes")
	}
}
