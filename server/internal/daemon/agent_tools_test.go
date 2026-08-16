package daemon

import (
	"encoding/json"
	"testing"
)

func TestDecodeAllowedTools(t *testing.T) {
	tests := []struct {
		name       string
		raw        json.RawMessage
		configured bool
		want       []string
		wantErr    bool
	}{
		{name: "omitted", raw: nil},
		{name: "null", raw: json.RawMessage("null")},
		{name: "deny all", raw: json.RawMessage("[]"), configured: true, want: []string{}},
		{name: "patterns", raw: json.RawMessage(`["mcp__builderlync__get_contact","Bash(multica issue *)"]`), configured: true, want: []string{"mcp__builderlync__get_contact", "Bash(multica issue *)"}},
		{name: "object rejected", raw: json.RawMessage(`{"tools":[]}`), wantErr: true},
		{name: "non string rejected", raw: json.RawMessage(`["ok", 7]`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, configured, err := decodeAllowedTools(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeAllowedTools() error = %v, wantErr %v", err, tt.wantErr)
			}
			if configured != tt.configured {
				t.Fatalf("configured = %v, want %v", configured, tt.configured)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("tools = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("tools = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}
