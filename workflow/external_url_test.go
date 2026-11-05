package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/omnideck-dev/cli/config"
)

func TestCheckExternalURL(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", WebUIPort: "2339"}
	for _, tc := range []struct {
		name string
		env  []string
		want ExternalURLState
	}{
		{"missing", []string{"PORT=8080"}, ExternalURLMissing},
		{"no environment", nil, ExternalURLMissing},
		{"empty value", []string{"OMNIDECK_EXTERNAL_URL="}, ExternalURLMissing},
		{"wrong port", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:9999"}, ExternalURLMismatch},
		{"remote origin", []string{"OMNIDECK_EXTERNAL_URL=https://host.tailnet.ts.net"}, ExternalURLMismatch},
		{"trailing slash", []string{"OMNIDECK_EXTERNAL_URL=http://localhost:2339/"}, ExternalURLOK},
		{"surrounding whitespace", []string{"OMNIDECK_EXTERNAL_URL= http://localhost:2339 "}, ExternalURLOK},
		{"correct", []string{"PORT=8080", "OMNIDECK_EXTERNAL_URL=http://localhost:2339"}, ExternalURLOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check, err := CheckExternalURL(&fakeContainerEngine{env: tc.env}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if check.State != tc.want || check.Expected != "http://localhost:2339" {
				t.Fatalf("check = %#v, want state %s", check, tc.want)
			}
		})
	}
}

// An unreadable old container must not be reported as "set": it may already
// have had the right value.
func TestExternalURLBeforeReplaceTreatsInspectionFailureAsUnknown(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", WebUIPort: "2339"}
	check := ExternalURLBeforeReplace(&fakeContainerEngine{envErr: errors.New("inspect timed out")}, cfg)
	if check.State != ExternalURLUnknown || check.Expected != cfg.BrowserURL() {
		t.Fatalf("check = %#v", check)
	}
}

func TestVerifyExternalURLReportsChange(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", WebUIPort: "2339"}
	eng := &fakeContainerEngine{env: []string{"OMNIDECK_EXTERNAL_URL=http://localhost:2339"}}
	for _, tc := range []struct {
		before  ExternalURLCheck
		want    ExternalURLChange
		summary string
	}{
		{ExternalURLCheck{State: ExternalURLMissing}, ExternalURLSet, "set to http://localhost:2339"},
		{ExternalURLCheck{State: ExternalURLMismatch, Actual: "http://localhost:9999"}, ExternalURLCorrected, "corrected http://localhost:9999 → http://localhost:2339"},
		{ExternalURLCheck{State: ExternalURLOK, Actual: "http://localhost:2339"}, ExternalURLConfirmed, "confirmed http://localhost:2339"},
		{ExternalURLCheck{State: ExternalURLUnknown}, ExternalURLVerified, "verified http://localhost:2339 (the previous value could not be read)"},
	} {
		report, err := VerifyExternalURL(eng, cfg, tc.before)
		if err != nil {
			t.Fatal(err)
		}
		if report.Change != tc.want || report.Summary() != tc.summary {
			t.Fatalf("report = %#v summary %q, want %s %q", report, report.Summary(), tc.want, tc.summary)
		}
	}
}

func TestVerifyExternalURLFailsLoudlyWhenReplacementIsWrong(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", WebUIPort: "2339"}
	_, err := VerifyExternalURL(&fakeContainerEngine{env: []string{"PORT=8080"}}, cfg, ExternalURLCheck{State: ExternalURLMissing})
	if !errors.Is(err, ErrExternalURL) || !strings.Contains(err.Error(), "http://localhost:2339") {
		t.Fatalf("err = %v, want ErrExternalURL naming the expected address", err)
	}
}

func TestUpdateTargetRecordsCurrentContainerLayout(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", Image: config.DefaultImage}
	current, next := UpdateTarget(cfg)
	if current.LayoutVersion != 0 || cfg.LayoutVersion != 0 {
		t.Fatal("UpdateTarget must not change the saved configuration it was given")
	}
	if next.LayoutVersion != config.CurrentContainerLayout {
		t.Fatalf("next.LayoutVersion = %d, want %d", next.LayoutVersion, config.CurrentContainerLayout)
	}
}

func TestRunOptionsExternalURLMatchesBrowserURL(t *testing.T) {
	cfg := &config.Config{ContainerName: "work", WebUIPort: "2339"}
	if got := RunOptions(cfg).ExternalURL; got != cfg.BrowserURL() {
		t.Fatalf("ExternalURL = %q, want %q", got, cfg.BrowserURL())
	}
}
