package runtimepool

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseQuickCreateContextStrictTypedMarker(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantRecognized bool
		wantErr        bool
		wantPrompt     string
	}{
		{
			name: "complete supported marker",
			raw: `{
				"type":"quick_create",
				"schema_version":"multica.quick-create/v1",
				"prompt":"Create the release issue",
				"requester_id":"10000000-0000-4000-8000-000000000001",
				"workspace_id":"10000000-0000-4000-8000-000000000002",
				"priority":"high",
				"due_date":"2026-08-20",
				"project_id":"10000000-0000-4000-8000-000000000003",
				"squad_id":"10000000-0000-4000-8000-000000000004",
				"attachment_ids":["10000000-0000-4000-8000-000000000005"],
				"parent_issue_id":"10000000-0000-4000-8000-000000000006"
			}`,
			wantRecognized: true,
			wantPrompt:     "Create the release issue",
		},
		{name: "ordinary context", raw: `{"head_sha":"abc"}`},
		{name: "other typed context", raw: `{"type":"future_job","unknown":true}`},
		{name: "malformed json", raw: `{"type":`, wantErr: true},
		{name: "legacy marker without schema", raw: `{"type":"quick_create","prompt":"old"}`, wantRecognized: true, wantErr: true},
		{name: "unsupported schema", raw: `{"type":"quick_create","schema_version":"multica.quick-create/v2","prompt":"future"}`, wantRecognized: true, wantErr: true},
		{name: "recognized marker with unknown field", raw: `{"type":"quick_create","schema_version":"multica.quick-create/v1","prompt":"x","surprise":true}`, wantRecognized: true, wantErr: true},
		{name: "recognized marker with duplicate field", raw: `{"type":"quick_create","type":"quick_create","schema_version":"multica.quick-create/v1"}`, wantRecognized: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, recognized, err := ParseQuickCreateContext([]byte(tt.raw))
			if recognized != tt.wantRecognized {
				t.Fatalf("recognized = %v, want %v", recognized, tt.wantRecognized)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && value.Prompt != tt.wantPrompt {
				t.Fatalf("prompt = %q, want %q", value.Prompt, tt.wantPrompt)
			}
		})
	}
}

func TestRuntimeSupportsPoolQuickCreate(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		metadata string
		want     bool
	}{
		{name: "ordinary task ignores CLI version", context: `{"head_sha":"abc"}`, metadata: `{}`, want: true},
		{name: "base exact floor", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1","prompt":"x"}`, metadata: `{"cli_version":"0.2.21"}`, want: true},
		{name: "base too old", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1","prompt":"x"}`, metadata: `{"cli_version":"0.2.20"}`},
		{name: "fields require higher floor", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1","priority":"high"}`, metadata: `{"cli_version":"0.4.2"}`},
		{name: "fields exact floor", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1","due_date":"2026-08-20"}`, metadata: `{"cli_version":"0.4.3"}`, want: true},
		{name: "missing version fails closed", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1"}`, metadata: `{}`},
		{name: "malformed marker fails closed", context: `{"type":"quick_create"`, metadata: `{"cli_version":"9.0.0"}`},
		{name: "legacy marker fails closed", context: `{"type":"quick_create","prompt":"old"}`, metadata: `{"cli_version":"9.0.0"}`},
		{name: "unknown recognized field fails closed", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1","unknown":true}`, metadata: `{"cli_version":"9.0.0"}`},
		{name: "malformed metadata fails closed", context: `{"type":"quick_create","schema_version":"multica.quick-create/v1"}`, metadata: `{"cli_version":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimeSupportsPoolQuickCreate(
				db.AgentTaskQueue{Context: []byte(tt.context)},
				db.AgentRuntime{Metadata: []byte(tt.metadata)},
			)
			if got != tt.want {
				t.Fatalf("runtimeSupportsPoolQuickCreate() = %v, want %v", got, tt.want)
			}
		})
	}
}
