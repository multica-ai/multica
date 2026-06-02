package mcpvalidate

import (
	"encoding/json"
	"testing"
)

func TestValidate_NilOrEmpty(t *testing.T) {
	if err := Validate(nil); err != nil {
		t.Errorf("nil: got %v, want nil", err)
	}
	if err := Validate(json.RawMessage("")); err != nil {
		t.Errorf("empty: got %v, want nil", err)
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	if err := Validate(json.RawMessage("{not json}")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidate_NotObject(t *testing.T) {
	if err := Validate(json.RawMessage(`"hello"`)); err == nil {
		t.Error("expected error for string")
	}
	if err := Validate(json.RawMessage(`[1,2,3]`)); err == nil {
		t.Error("expected error for array")
	}
}

func TestValidate_MissingMcpServers(t *testing.T) {
	if err := Validate(json.RawMessage(`{"other": "key"}`)); err == nil {
		t.Error("expected error for missing mcpServers")
	}
}

func TestValidate_EmptyMcpServers(t *testing.T) {
	if err := Validate(json.RawMessage(`{"mcpServers": {}}`)); err != nil {
		t.Errorf("empty mcpServers: got %v, want nil", err)
	}
}

func TestValidate_StdioEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid minimal stdio",
			input:   `{"mcpServers": {"fs": {"command": "npx"}}}`,
			wantErr: false,
		},
		{
			name:    "valid stdio with args and env",
			input:   `{"mcpServers": {"fs": {"command": "npx", "args": ["-y", "server"], "env": {"KEY": "val"}}}}`,
			wantErr: false,
		},
		{
			name:    "empty command rejected",
			input:   `{"mcpServers": {"fs": {"command": ""}}}`,
			wantErr: true,
		},
		{
			name:    "non-string command rejected",
			input:   `{"mcpServers": {"fs": {"command": 123}}}`,
			wantErr: true,
		},
		{
			name:    "non-array args rejected",
			input:   `{"mcpServers": {"fs": {"command": "npx", "args": "bad"}}}`,
			wantErr: true,
		},
		{
			name:    "non-string arg rejected",
			input:   `{"mcpServers": {"fs": {"command": "npx", "args": [123]}}}`,
			wantErr: true,
		},
		{
			name:    "non-object env rejected",
			input:   `{"mcpServers": {"fs": {"command": "npx", "env": "bad"}}}`,
			wantErr: true,
		},
		{
			name:    "non-string env value rejected",
			input:   `{"mcpServers": {"fs": {"command": "npx", "env": {"KEY": 123}}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(json.RawMessage(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_HTTPEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid SSE",
			input:   `{"mcpServers": {"api": {"type": "sse", "url": "https://example.com/mcp"}}}`,
			wantErr: false,
		},
		{
			name:    "valid streamable-http",
			input:   `{"mcpServers": {"api": {"type": "streamable-http", "url": "https://example.com/mcp"}}}`,
			wantErr: false,
		},
		{
			name:    "valid with headers",
			input:   `{"mcpServers": {"api": {"type": "sse", "url": "https://x.com", "headers": {"Auth": "Bearer tok"}}}}`,
			wantErr: false,
		},
		{
			name:    "invalid type rejected",
			input:   `{"mcpServers": {"api": {"type": "websocket", "url": "https://x.com"}}}`,
			wantErr: true,
		},
		{
			name:    "missing url rejected",
			input:   `{"mcpServers": {"api": {"type": "sse"}}}`,
			wantErr: true,
		},
		{
			name:    "empty url rejected",
			input:   `{"mcpServers": {"api": {"type": "sse", "url": ""}}}`,
			wantErr: true,
		},
		{
			name:    "non-string headers value rejected",
			input:   `{"mcpServers": {"api": {"type": "sse", "url": "https://x.com", "headers": {"Auth": 123}}}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(json.RawMessage(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_MixedServers(t *testing.T) {
	input := `{
		"mcpServers": {
			"fs": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]},
			"api": {"type": "sse", "url": "https://example.com/mcp", "headers": {"Authorization": "Bearer token"}}
		}
	}`
	if err := Validate(json.RawMessage(input)); err != nil {
		t.Errorf("valid mixed config: got %v, want nil", err)
	}
}

func TestValidate_UnknownTopLevelKey(t *testing.T) {
	input := `{"mcpServers": {"fs": {"command": "npx"}}, "unknown": true}`
	// Top-level unknown keys are allowed for forward-compat.
	if err := Validate(json.RawMessage(input)); err != nil {
		t.Errorf("unknown top-level key: got %v, want nil", err)
	}
}

func TestValidate_EntryNotObject(t *testing.T) {
	input := `{"mcpServers": {"fs": "npx"}}`
	if err := Validate(json.RawMessage(input)); err == nil {
		t.Error("expected error for non-object entry")
	}
}
