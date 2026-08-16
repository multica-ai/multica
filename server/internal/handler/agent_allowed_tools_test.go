package handler

import (
	"encoding/json"
	"testing"
)

func TestParseAllowedTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []string
		wantSet bool
		wantErr bool
	}{
		{name: "omitted", wantSet: false},
		{name: "null", raw: "null", wantSet: false},
		{name: "empty explicit deny", raw: "[]", want: []string{}, wantSet: true},
		{name: "patterns", raw: `["mcp__builderlync__get_contact","Bash(multica issue *)"]`, want: []string{"mcp__builderlync__get_contact", "Bash(multica issue *)"}, wantSet: true},
		{name: "object rejected", raw: `{"mcp__builderlync__get_contact":true}`, wantErr: true},
		{name: "empty name rejected", raw: `[""]`, wantErr: true},
		{name: "duplicate rejected", raw: `["Read","Read"]`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "" {
				raw = json.RawMessage(tt.raw)
			}
			got, gotSet, err := parseAllowedTools(raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAllowedTools() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if gotSet != tt.wantSet {
				t.Fatalf("configured = %v, want %v", gotSet, tt.wantSet)
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
