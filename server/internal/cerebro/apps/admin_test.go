package apps

import (
	"os"
	"strings"
	"testing"
)

func TestMiniAppLifecyclePermissionsAndCascadeDeletion(t *testing.T) {
	for _, capability := range []string{"apps.create", "apps.manage", "apps.delete"} {
		if !isKnownAppCapability(capability) {
			t.Errorf("lifecycle permission %q is not recognized", capability)
		}
	}
	if isKnownAppCapability("use_apps") {
		t.Fatal("legacy app permission is still accepted")
	}
	raw, err := os.ReadFile("../../../migrations/9135_cerebro_mini_apps.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, table := range []string{"cerebro_app_version", "cerebro_app_kv"} {
		if !strings.Contains(schema, "REFERENCES cerebro_app(id) ON DELETE CASCADE") {
			t.Fatalf("%s is not covered by app cascade deletion", table)
		}
	}
}

func TestAllergenFormatterIsAnInstallablePublishedBuiltin(t *testing.T) {
	if allergenFormatterID.String() != "f1540000-0000-4154-8154-000000000001" {
		t.Fatalf("unexpected FIR-154 app id %s", allergenFormatterID)
	}
	if !strings.Contains(string(allergenFormatterSnapshot), `"Allergen Formatter"`) || !strings.Contains(string(allergenFormatterSnapshot), `"integration"`) {
		t.Fatal("FIR-154 snapshot is not the real scoped app bundle")
	}
}
