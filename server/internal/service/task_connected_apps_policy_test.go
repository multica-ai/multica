package service

import (
	"encoding/json"
	"testing"
)

func TestPreserveDisabledConnectedApps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata json.RawMessage
		disabled bool
	}{
		{name: "explicit deny marker", metadata: json.RawMessage("[]"), disabled: true},
		{name: "deny marker with whitespace", metadata: json.RawMessage(" \n [] \t"), disabled: true},
		{name: "unresolved metadata", metadata: nil, disabled: false},
		{name: "resolved apps", metadata: json.RawMessage(`[{"provider":"composio"}]`), disabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			overlay, disabled := preserveDisabledConnectedApps(tt.metadata)
			if disabled != tt.disabled {
				t.Fatalf("disabled = %v, want %v", disabled, tt.disabled)
			}
			if disabled && string(overlay.ConnectedApps) != "[]" {
				t.Fatalf("preserved metadata = %q, want []", overlay.ConnectedApps)
			}
			if !disabled && (len(overlay.Overlay) != 0 || len(overlay.ConnectedApps) != 0) {
				t.Fatalf("non-disabled metadata produced overlay: %+v", overlay)
			}
		})
	}
}
