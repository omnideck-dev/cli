package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeArchiveName(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "omnideck", want: true},
		{name: "./omnideck", want: false},
		{name: "bin/omnideck", want: false},
		{name: "../omnideck", want: false},
	} {
		if got := safeArchiveName(test.name, "omnideck"); got != test.want {
			t.Errorf("safeArchiveName(%q) = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestExtractZipRejectsExtraFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range []string{"omnideck.exe", "unexpected.txt"} {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte("fixture")); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := extractReleaseArchive(path, "windows"); err == nil {
		t.Fatal("archive with an extra file was accepted")
	}
}

func TestDecodeSingleJSONRejectsTrailingValue(t *testing.T) {
	var payload map[string]any
	if err := decodeSingleJSON("{\"ok\":true}\n{\"extra\":true}\n", &payload); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
}

func TestVersionSchemaRejectsMissingContractVersion(t *testing.T) {
	contractsDir := filepath.Join("..", "..", "contracts")
	invalid := `{"version":"test","commit":"abc","date":"today"}`
	if err := validateJSONSchema(contractsDir, "json/v3/version.schema.json", invalid); err == nil {
		t.Fatal("version schema accepted a payload without jsonContract")
	}
	valid := `{"version":"test","commit":"abc","date":"today","jsonContract":3}`
	if err := validateJSONSchema(contractsDir, "json/v3/version.schema.json", valid); err != nil {
		t.Fatalf("version schema rejected a valid payload: %v", err)
	}
}

func TestJSONContract3AcceptsTypedRuntimeSetupEventsAndErrors(t *testing.T) {
	contractsDir := filepath.Join("..", "..", "contracts")
	errorEnvelope := `{"error":{"code":"PERMISSION_CANCELLED","message":"approval cancelled","detail":"ERROR_CANCELLED (1223)"}}`
	if err := validateJSONSchema(contractsDir, "json/v3/error.schema.json", errorEnvelope); err != nil {
		t.Fatalf("contract 3 rejected a typed setup error: %v", err)
	}
	event := `{"stage":"software","substage":"wsl-permission","state":"permission","activity":"Getting ready","status":"Approval required"}`
	if err := validateJSONSchema(contractsDir, "json/v3/stage-event.schema.json", event); err != nil {
		t.Fatalf("contract 3 rejected a runtime setup event: %v", err)
	}
}
