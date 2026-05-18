package daemonmcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	cases := []struct {
		name    string
		runtime string
		agent   string
		want    string // canonical JSON shape; we re-marshal to compare
	}{
		{
			name:    "both empty returns nil",
			runtime: "",
			agent:   "",
			want:    "",
		},
		{
			name:    "only runtime",
			runtime: `{"mcpServers":{"browser":{"command":"npx","args":["@x/y"]}}}`,
			agent:   "",
			want:    `{"mcpServers":{"browser":{"args":["@x/y"],"command":"npx"}}}`,
		},
		{
			name:    "only agent",
			runtime: "",
			agent:   `{"mcpServers":{"foo":{"command":"foo"}}}`,
			want:    `{"mcpServers":{"foo":{"command":"foo"}}}`,
		},
		{
			name:    "agent overrides runtime per server name",
			runtime: `{"mcpServers":{"browser":{"command":"npx","args":["@v1"]}}}`,
			agent:   `{"mcpServers":{"browser":{"command":"npx","args":["@v2"]}}}`,
			want:    `{"mcpServers":{"browser":{"args":["@v2"],"command":"npx"}}}`,
		},
		{
			name:    "agent extends runtime with new server",
			runtime: `{"mcpServers":{"browser":{"command":"npx","args":["@x"]}}}`,
			agent:   `{"mcpServers":{"db":{"command":"psql"}}}`,
			want:    `{"mcpServers":{"browser":{"args":["@x"],"command":"npx"},"db":{"command":"psql"}}}`,
		},
		{
			name:    "agent without mcpServers preserves runtime servers",
			runtime: `{"mcpServers":{"browser":{"command":"npx"}}}`,
			agent:   `{"otherField":"x"}`,
			want:    `{"mcpServers":{"browser":{"command":"npx"}},"otherField":"x"}`,
		},
		{
			name:    "malformed runtime falls back to agent",
			runtime: `not json`,
			agent:   `{"mcpServers":{"foo":{"command":"foo"}}}`,
			want:    `{"mcpServers":{"foo":{"command":"foo"}}}`,
		},
		{
			name:    "malformed agent falls back to runtime",
			runtime: `{"mcpServers":{"foo":{"command":"foo"}}}`,
			agent:   `garbage`,
			want:    `{"mcpServers":{"foo":{"command":"foo"}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Merge(json.RawMessage(tc.runtime), json.RawMessage(tc.agent))
			if tc.want == "" {
				if got != nil {
					t.Fatalf("want nil, got %s", string(got))
				}
				return
			}
			var gotObj, wantObj any
			if err := json.Unmarshal(got, &gotObj); err != nil {
				t.Fatalf("unmarshal got: %v (%s)", err, string(got))
			}
			if err := json.Unmarshal([]byte(tc.want), &wantObj); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			if !reflect.DeepEqual(gotObj, wantObj) {
				t.Fatalf("merge mismatch\n  got:  %s\n  want: %s", string(got), tc.want)
			}
		})
	}
}
