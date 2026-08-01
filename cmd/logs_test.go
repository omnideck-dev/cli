package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func newLogsFollowTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "logs"}
	cmd.Flags().BoolP("follow", "f", true, "")
	return cmd
}

func TestResolveLogsFollowDefaultsFalseUnderJSONWhenNotExplicit(t *testing.T) {
	cmd := newLogsFollowTestCmd(t)
	if got := resolveLogsFollow(cmd, true, true); got {
		t.Fatal("logs --json with --follow unset should default to false")
	}
}

func TestResolveLogsFollowExplicitFlagWinsUnderJSON(t *testing.T) {
	cmd := newLogsFollowTestCmd(t)
	if err := cmd.Flags().Set("follow", "true"); err != nil {
		t.Fatal(err)
	}
	if got := resolveLogsFollow(cmd, true, true); !got {
		t.Fatal("explicit --follow=true under --json must win over the default-false safety valve")
	}
}

func TestResolveLogsFollowExplicitFalseUnderJSON(t *testing.T) {
	cmd := newLogsFollowTestCmd(t)
	if err := cmd.Flags().Set("follow", "false"); err != nil {
		t.Fatal(err)
	}
	if got := resolveLogsFollow(cmd, true, false); got {
		t.Fatal("explicit --follow=false under --json must stay false")
	}
}

func TestResolveLogsFollowUnaffectedWithoutJSON(t *testing.T) {
	cmd := newLogsFollowTestCmd(t)
	if got := resolveLogsFollow(cmd, false, true); !got {
		t.Fatal("non-json --follow default (true) must be unchanged")
	}
}
