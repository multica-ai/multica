package service

import (
	"strings"
	"testing"
)

func TestCompanyExportRefusesAnotherSchemaVersion(t *testing.T) {
	err := ValidateCompanyExport(CompanyExport{SchemaVersion: CompanyExportVersion + 1})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("a newer schema is refused, got %v", err)
	}
}

func TestCompanyExportRefusesDuplicateNames(t *testing.T) {
	doc := CompanyExport{SchemaVersion: CompanyExportVersion, Agents: []ExportAgent{{Name: "A"}, {Name: "A"}}}
	if err := ValidateCompanyExport(doc); err == nil {
		t.Fatal("two agents with one name are refused: names are the identity")
	}
	doc = CompanyExport{SchemaVersion: CompanyExportVersion, Squads: []ExportSquad{{Name: "S"}, {Name: "S"}}}
	if err := ValidateCompanyExport(doc); err == nil {
		t.Fatal("two squads with one name are refused")
	}
}

func TestCompanyExportNamesWhatItExcludes(t *testing.T) {
	joined := strings.Join(CompanyExportExcluded, ",")
	for _, want := range []string{"secrets", "runtimes", "documents", "history"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the export must name %q as excluded", want)
		}
	}
	if !strings.Contains(CompanyExportRetentionNote, "90 days") {
		t.Error("the export carries the retention note")
	}
}
