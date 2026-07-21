package main

import (
	"strings"
	"testing"
)

func TestValidateExternalUpsertWorkspaceFailsClosed(t *testing.T) {
	const configured = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if err := validateExternalUpsertWorkspace(map[string]any{"workspace_id": configured}, configured); err != nil {
		t.Fatalf("matching workspace rejected: %v", err)
	}
	for name, result := range map[string]map[string]any{
		"missing":  {},
		"wrong":    {"workspace_id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
		"non_text": {"workspace_id": 123},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateExternalUpsertWorkspace(result, configured)
			if err == nil || !strings.Contains(err.Error(), "workspace") {
				t.Fatalf("error = %v, want workspace mismatch", err)
			}
		})
	}
	if err := validateExternalUpsertWorkspace(map[string]any{"workspace_id": configured}, ""); err == nil {
		t.Fatal("empty configured workspace accepted")
	}
}
