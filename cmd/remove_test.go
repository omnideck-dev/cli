package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestRemoveIsTopLevelWithUninstallAlias(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"remove"})
	if err != nil {
		t.Fatal(err)
	}
	if command != removeCmd {
		t.Fatalf("remove resolves to %q", command.CommandPath())
	}
	command, _, err = rootCmd.Find([]string{"uninstall"})
	if err != nil {
		t.Fatal(err)
	}
	if command != removeCmd {
		t.Fatalf("uninstall alias resolves to %q, want the remove command", command.CommandPath())
	}
	if _, _, err := rootCmd.Find([]string{"instance", "remove"}); err == nil {
		t.Fatal("the old `instance remove` subcommand group should no longer exist")
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
