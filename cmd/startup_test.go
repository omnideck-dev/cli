package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/workflow"
)

func TestStartupFailureHasMatchingTextAndJSONDiagnostics(t *testing.T) {
	err := &workflow.StartupFailure{Code: "MIGRATION_FAILED", Phase: "migrations", Step: "broker:001_operation_grants"}
	structured := environmentJSONError(err)
	if structured.payload.Code != "MIGRATION_FAILED" || !strings.Contains(structured.payload.Detail, err.Step) || !strings.Contains(structured.payload.Hint, "logs") {
		t.Fatalf("payload: %+v", structured.payload)
	}
	text := startupTextError(err).Error()
	for _, part := range []string{"saved settings", err.Step, "omnideck logs"} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in %s", part, text)
		}
	}
	if _, err := json.Marshal(structured.payload); err != nil {
		t.Fatal(err)
	}
}
